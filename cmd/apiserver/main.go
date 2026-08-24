/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command apiserver serves the benchmark web UI's backend against the
// management cluster's Kubernetes API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// so a kubeconfig using them works when run outside the cluster.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/api"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(benchv1alpha1.AddToScheme(scheme))
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "apiserver:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		addr            string
		namespace       string
		prometheusURL   string
		grafanaURL      string
		tokenSecretName string
		corsOrigin      string
	)
	flag.StringVar(&addr, "bind-address", ":8080", "The address the web API binds to.")
	flag.StringVar(&namespace, "namespace", envOr("BENCH_NAMESPACE", "authentik-bench"),
		"The namespace bench CRs are read from and written to.")
	flag.StringVar(&prometheusURL, "prometheus-url", os.Getenv("BENCH_PROMETHEUS_URL"),
		"Base URL of the Prometheus the PromQL proxy forwards to.")
	flag.StringVar(&grafanaURL, "grafana-url", os.Getenv("BENCH_GRAFANA_URL"),
		"Browser-reachable Grafana base URL used to build dashboard embed URLs.")
	flag.StringVar(&tokenSecretName, "token-secret", os.Getenv("BENCH_API_TOKEN_SECRET"),
		"Name of a Secret in --namespace holding the UI bearer token under key \"token\". "+
			"Overridden by BENCH_API_TOKEN when that is set.")
	flag.StringVar(&corsOrigin, "cors-origin", os.Getenv("BENCH_CORS_ORIGIN"),
		"Origin allowed to call the API cross-origin, for running the frontend dev server against a remote backend.")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("apiserver")

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("build client: %w", err)
	}

	ctx := ctrl.SetupSignalHandler()
	token, err := resolveToken(ctx, c, namespace, tokenSecretName)
	if err != nil {
		return err
	}

	capiAvailable := hasCAPI(cfg)
	if !capiAvailable {
		log.Info("Cluster API is not installed; /clusters reports the management cluster only")
	}

	srv := api.New(c, api.Config{
		Namespace:     namespace,
		Token:         token,
		PrometheusURL: prometheusURL,
		GrafanaURL:    grafanaURL,
		CAPIAvailable: capiAvailable,
	})

	handler := srv.Router()
	if corsOrigin != "" {
		handler = withCORS(handler, corsOrigin)
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error(err, "Failed to shut down cleanly")
		}
	}()

	log.Info("Serving web API", "address", addr, "namespace", namespace)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// resolveToken loads the shared bearer token, preferring the environment (for
// local development) over the Secret the deployment mounts. Starting without
// one is refused rather than defaulted: an unauthenticated backend has full
// write access to every benchmark CRD.
func resolveToken(ctx context.Context, c client.Client, namespace, secretName string) (string, error) {
	if token := os.Getenv("BENCH_API_TOKEN"); token != "" {
		return token, nil
	}
	if secretName == "" {
		return "", errors.New("no API token configured: set BENCH_API_TOKEN or --token-secret")
	}
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &secret); err != nil {
		return "", fmt.Errorf("read token secret %s/%s: %w", namespace, secretName, err)
	}
	token := strings.TrimSpace(string(secret.Data["token"]))
	if token == "" {
		return "", fmt.Errorf("secret %s/%s has no non-empty \"token\" key", namespace, secretName)
	}
	return token, nil
}

// hasCAPI reports whether cluster.x-k8s.io Clusters are servable, which is
// what decides whether /clusters can list anything beyond the management
// cluster itself.
func hasCAPI(cfg *rest.Config) bool {
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, nil)
	if err != nil {
		return false
	}
	_, err = mapper.RESTMapping(schema.GroupKind{Group: "cluster.x-k8s.io", Kind: "Cluster"}, "v1beta2")
	return err == nil
}

// withCORS allows the Next.js dev server to call a backend on another origin.
// Deliberately narrow: one configured origin, the methods this API uses, and
// the Authorization header.
func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

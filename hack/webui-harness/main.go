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

// Command webui-harness stands up everything the web UI needs to be driven by
// a browser, without a Kubernetes cluster: an envtest control plane with the
// bench CRDs installed, a stub Prometheus answering the compare view's
// queries, and the real web API served against both.
//
// It exists because the UI's end-to-end behaviour - can you create a topology,
// create a test case, start a run, and see the result - depends on a real API
// server's validation and defaulting, but not on anything a controller does.
// envtest gives exactly that, on a machine with no cluster.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/api"
)

const (
	namespace = "authentik-bench"
	token     = "e2e-token"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "webui-harness:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(benchv1alpha1.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(root, "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	if err != nil {
		return fmt.Errorf("start envtest (is KUBEBUILDER_ASSETS set? run via make): %w", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "webui-harness: stopping envtest:", err)
		}
	}()

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := seed(ctx, c); err != nil {
		return fmt.Errorf("seed fixtures: %w", err)
	}

	prometheus := stubPrometheus()
	defer prometheus.Close()

	srv := api.New(c, api.Config{
		Namespace:     namespace,
		Token:         token,
		PrometheusURL: prometheus.URL(),
		GrafanaURL:    "http://127.0.0.1:3001",
	})
	httpServer := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Println("webui-harness: API on http://127.0.0.1:8080, token", token)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// seed creates the namespace and one topology that already reports Ready.
// Nothing reconciles here, so a topology that stayed Pending forever would
// make every downstream page untestable.
func seed(ctx context.Context, c client.Client) error {
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil {
		return err
	}

	topology := &benchv1alpha1.AuthentikTopology{
		ObjectMeta: metav1.ObjectMeta{Name: "seeded-topology", Namespace: namespace},
		Spec: benchv1alpha1.AuthentikTopologySpec{
			Namespace: "ak-seeded",
			Server:    benchv1alpha1.WorkloadSpec{Replicas: 2},
			Worker:    benchv1alpha1.WorkloadSpec{Replicas: 1},
			Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
		},
	}
	if err := c.Create(ctx, topology); err != nil {
		return err
	}
	topology.Status = benchv1alpha1.AuthentikTopologyStatus{
		ServiceEndpoint: "authentik-seeded-server.ak-seeded.svc.cluster.local:80",
		Conditions: []metav1.Condition{{
			Type:               benchv1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             benchv1alpha1.ReasonReady,
			Message:            "Seeded by the web UI harness",
			LastTransitionTime: metav1.Now(),
		}},
	}
	return c.Status().Update(ctx, topology)
}

// stubPrometheus answers instant queries with a value derived from the test
// being asked about, so the compare table shows two visibly different columns
// rather than the same number twice.
type promStub struct {
	server *http.Server
	addr   string
}

func stubPrometheus() *promStub {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		value := 100.0
		if strings.Contains(query, "login-b") {
			value = 42.0
		}
		if strings.Contains(query, "k6_checks_rate") || strings.Contains(query, "k6_http_req_failed_rate") {
			value /= 1000
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{map[string]any{
					"metric": map[string]string{},
					"value":  []any{time.Now().Unix(), fmt.Sprintf("%g", value)},
				}},
			},
		})
	})
	server := &http.Server{Addr: "127.0.0.1:9099", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "webui-harness: stub Prometheus:", err)
		}
	}()
	return &promStub{server: server, addr: "http://127.0.0.1:9099"}
}

func (p *promStub) URL() string { return p.addr }

func (p *promStub) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.server.Shutdown(ctx)
}

// repoRoot walks up from the working directory to the module root, so the
// harness can be started from anywhere.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find the module root (no go.mod in any parent directory)")
		}
		dir = parent
	}
}

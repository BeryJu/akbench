package render

import (
	"encoding/json"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// HelmRepositoryName is shared across every AuthentikTopology, since they all
// install from the same chart repository.
const HelmRepositoryName = "authentik"

const defaultChartRepo = "https://charts.goauthentik.io"

// HelmRepository returns the (singleton, namespace-scoped) HelmRepository
// pointing at the authentik chart repo. Callers apply it once per namespace
// and share it across topologies.
func HelmRepository(namespace string, spec benchv1alpha1.ChartSpec) *sourcev1.HelmRepository {
	url := spec.Repo
	if url == "" {
		url = defaultChartRepo
	}
	return &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HelmRepositoryName,
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL:      url,
			Interval: metav1.Duration{Duration: helmRepoInterval},
		},
	}
}

const helmRepoInterval = 10 * time.Minute

// AuthentikTopologyHelmValues is what AuthentikTopologyHelmRelease renders
// into spec.values. Exported so unit/contract tests can assert on the
// concrete shape without re-parsing JSON.
type AuthentikTopologyHelmValues struct {
	Global    map[string]any `json:"global,omitempty"`
	Server    map[string]any `json:"server"`
	Worker    map[string]any `json:"worker"`
	Authentik map[string]any `json:"authentik"`
	Postgres  map[string]any `json:"postgresql"`
}

// HelmValues builds the authentik chart values for a topology, per the
// chart's actual schema (charts.goauthentik.io/authentik, values.yaml):
// global.image.{repository,tag}, server/worker.{replicas,resources,
// nodeSelector,tolerations,affinity}, authentik.{log_level,error_reporting,
// postgresql.*,existingSecret}, and top-level postgresql.enabled/auth for the
// bundled subchart.
//
// authentikSecretName must name a Secret (created by the caller, see
// internal/controller's ensureRandomSecret) holding AUTHENTIK_SECRET_KEY -
// the chart does not generate one itself, and authentik refuses to start
// without it. pgConn carries the resolved Postgres connection: for Embedded
// mode only Password is set (paired with the bundled subchart's own
// generated auth.password so both sides agree on the same value); for CNPG/
// External modes the full connection is set and the bundled subchart stays
// disabled.
//
// Only Embedded postgres and Default placement are wired up for M1; CNPG and
// External postgres, and Dedicated placement, are added in M2 (see the repo
// plan).
func HelmValues(t *benchv1alpha1.AuthentikTopology, authentikSecretName string, pgConn *PostgresConnection) (*apiextensionsv1.JSON, error) {
	embedded := t.Spec.Postgres.Mode == benchv1alpha1.PostgresEmbedded
	values := AuthentikTopologyHelmValues{
		Server: map[string]any{
			"replicas": t.Spec.Server.Replicas,
		},
		Worker: map[string]any{
			"replicas": t.Spec.Worker.Replicas,
		},
		Authentik: map[string]any{
			"existingSecret": map[string]any{"secretName": authentikSecretName},
		},
		Postgres: map[string]any{
			"enabled": embedded,
		},
	}

	if img := t.Spec.Authentik.Image; img != "" {
		values.Global = map[string]any{"image": map[string]any{
			"repository": img,
			"tag":        t.Spec.Authentik.Tag,
		}}
	}
	if t.Spec.Authentik.LogLevel != "" {
		values.Authentik["log_level"] = t.Spec.Authentik.LogLevel
	}
	if t.Spec.Authentik.ErrorReporting.Enabled {
		values.Authentik["error_reporting"] = map[string]any{
			"enabled": true,
		}
	}

	applyPlacement := func(w map[string]any) {
		if sel := NodeSelector(t.Spec.Placement); len(sel) > 0 {
			w["nodeSelector"] = sel
		}
		if tol := Tolerations(t.Spec.Placement); len(tol) > 0 {
			w["tolerations"] = tol
		}
		w["affinity"] = map[string]any{"podAntiAffinity": AffinityPreset(t.Spec.Placement)}
	}
	applyPlacement(values.Server)
	applyPlacement(values.Worker)

	switch {
	case embedded && pgConn != nil:
		// The bundled subchart auto-generates its own password unless told
		// otherwise; we pin it to our generated value so authentik's own
		// AUTHENTIK_POSTGRESQL__PASSWORD (below) actually matches it.
		values.Postgres["auth"] = map[string]any{"password": pgConn.Password}
		values.Authentik["postgresql"] = map[string]any{"password": pgConn.Password}
	case !embedded && pgConn != nil:
		values.Authentik["postgresql"] = map[string]any{
			"host":     pgConn.Host,
			"port":     pgConn.Port,
			"name":     pgConn.Database,
			"user":     pgConn.User,
			"password": pgConn.Password,
		}
	}

	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("marshal helm values: %w", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}, nil
}

// PostgresConnection is the resolved connection info passed to HelmValues.
// For Embedded mode only Password is meaningful (see above); for CNPG/
// External modes the full connection is read from the relevant Secret by the
// caller - the operator never manages the lifecycle of that Secret itself
// for External, and only reads CNPG's generated one for CNPG.
type PostgresConnection struct {
	Host     string
	Port     int32
	Database string
	User     string
	Password string
}

// HelmReleaseName is deterministic from the topology name, so the operator
// can look it up without storing it, though it's still recorded in
// status.helmReleaseRef for clarity.
func HelmReleaseName(t *benchv1alpha1.AuthentikTopology) string {
	return t.Name
}

// HelmRelease builds the Flux HelmRelease that installs the authentik chart
// for this topology. kubeConfigSecretName is empty for a topology on the
// management cluster itself (M1-M3); from M4 on it names the
// "<clusterRef>-kubeconfig" Secret CAPI produces, so Flux's helm-controller
// installs remotely.
func HelmRelease(t *benchv1alpha1.AuthentikTopology, kubeConfigSecretName string, values *apiextensionsv1.JSON) *helmv2.HelmRelease {
	version := t.Spec.Chart.Version
	if version == "" {
		version = "*"
	}
	hr := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HelmReleaseName(t),
			Namespace: t.Namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval: metav1.Duration{Duration: helmRepoInterval},
			// Explicit, rather than Flux's default "[TargetNamespace-]Name"
			// composition, so ServiceEndpoint's "<release>-server" naming
			// stays correct regardless of what the target namespace is
			// named.
			ReleaseName: HelmReleaseName(t),
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   "authentik",
					Version: version,
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      sourcev1.HelmRepositoryKind,
						Name:      HelmRepositoryName,
						Namespace: t.Namespace,
					},
				},
			},
			TargetNamespace: t.Spec.Namespace,
			Values:          values,
		},
	}
	if kubeConfigSecretName != "" {
		hr.Spec.KubeConfig = &fluxmeta.KubeConfigReference{
			SecretRef: &fluxmeta.SecretKeyReference{Name: kubeConfigSecretName},
		}
	}
	return hr
}

// ServiceEndpoint is the authentik chart's own service-name convention:
// "<release>-server" in the target namespace, serving HTTP on the chart's
// server.service.servicePortHttp (default 80). Verify against the deployed
// chart version at install time - this is the one place this plan's design
// flagged as needing confirmation against the real chart source.
func ServiceEndpoint(t *benchv1alpha1.AuthentikTopology) string {
	return fmt.Sprintf("%s-server.%s.svc.cluster.local:80", HelmReleaseName(t), t.Spec.Namespace)
}

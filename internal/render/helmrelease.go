package render

import (
	"encoding/json"
	"fmt"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// HelmRepositoryName is shared across every AuthentikTopology, since they all
// install from the same chart repository.
const HelmRepositoryName = "authentik"

// chartName is the chart to install from that repository - coincidentally
// the same string as HelmRepositoryName, but a different thing (the
// HelmRepository object's own name vs. the chart's name within it), so kept
// as its own constant rather than reusing HelmRepositoryName.
const chartName = "authentik"

// Helm values map keys the chart's own schema defines, repeated across
// server/worker/postgresql sections of HelmValues below.
const (
	valuesKeyReplicas = "replicas"
	valuesKeyPassword = "password"
	valuesKeyName     = "name"
)

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

const helmInstallTimeout = 15 * time.Minute

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
// global.{image,affinity}, server/worker.{replicas,resources,nodeSelector,
// tolerations}, authentik.{secret_key,log_level,error_reporting,
// postgresql.*}, and top-level postgresql.enabled/auth for the bundled
// subchart.
//
// secretKey is AUTHENTIK_SECRET_KEY's plaintext value (generated and cached
// by the caller, see internal/controller's ensureRandomSecret) - the chart
// does not generate one itself, and authentik refuses to start without it.
// pgConn carries the resolved Postgres connection: for Embedded mode only
// Password is set (paired with the bundled subchart's own generated
// auth.password so both sides agree on the same value); for CNPG/External
// modes the full connection is set and the bundled subchart stays disabled.
// pyroscopeHost/tracingEndpoint are the caller's already-defaulted-or-
// overridden values (see AuthentikTopologyReconciler's
// defaultPyroscopeHost/defaultTracingEndpoint) - neither has a values.yaml
// hook in the chart (verified against the chart's actual values.yaml: no
// pyroscope key at all, and error_reporting only has enabled/environment/
// send_pii, not an OTLP endpoint or sample rate), so both go in as raw
// AUTHENTIK_* env vars, the same names the Ansible harness this project
// replaces used (roles/authentik/templates/env.j2).
// withBlueprintVolume, if true, mounts a PVC (BlueprintPVCName) into the
// server pod at BlueprintMountPath so AuthentikTopologyReconciler's
// ApplyBlueprint can write a fixtures blueprint into it and exec
// `ak apply_blueprint` against it once the release is Ready. A volume at
// all (rather than exec writing straight into the image's own filesystem)
// is required for two independently-verified reasons: /blueprints itself
// isn't writable by the container's non-root user ("Permission denied" on a
// live pod), and ak apply_blueprint rejects any path outside
// CONFIG.blueprints_dir anyway ("Invalid blueprint path", also verified
// live). A PVC rather than a ConfigMap volume specifically because a
// benchmark-scale blueprint (thousands of synthetic users) can exceed a
// ConfigMap's ~1MiB etcd-backed size limit.
func HelmValues(t *benchv1alpha1.AuthentikTopology, secretKey string, pgConn *PostgresConnection, pyroscopeHost, tracingEndpoint string, withBlueprintVolume bool) (*apiextensionsv1.JSON, error) {
	embedded := t.Spec.Postgres.Mode == benchv1alpha1.PostgresEmbedded
	values := AuthentikTopologyHelmValues{
		Server: map[string]any{
			valuesKeyReplicas: t.Spec.Server.Replicas,
		},
		Worker: map[string]any{
			valuesKeyReplicas: t.Spec.Worker.Replicas,
		},
		// secret_key as a literal value, not authentik.existingSecret: the
		// chart only renders its authentik.env-derived config Secret (which
		// is where postgresql.*/log_level/error_reporting all actually get
		// baked into AUTHENTIK_* env vars) when existingSecret.secretName is
		// unset - existingSecret means "you own that whole secret yourself,"
		// not "just override the key" (verified against a live install of
		// this exact bug: postgresql.host silently never made it to the pod).
		Authentik: map[string]any{
			"secret_key": secretKey,
		},
		Postgres: map[string]any{
			"enabled": embedded,
		},
	}

	values.Global = map[string]any{
		// global.affinity.podAntiAffinity is the chart's *preset* mechanism
		// (_helpers.tpl's "authentik.affinity"). server.affinity/
		// worker.affinity are a different, raw-override knob: if set to
		// anything non-empty at all, the chart uses that value verbatim as
		// the Pod's real corev1.Affinity - a string like "hard" there fails
		// to unmarshal. The preset only has one, chart-wide value, which is
		// fine here since Dedicated/Default both apply symmetrically to
		// server and worker.
		"affinity": map[string]any{"podAntiAffinity": AffinityPreset(t.Spec.Placement)},
	}
	if img := t.Spec.Authentik.Image; img != "" {
		values.Global["image"] = map[string]any{
			"repository": img,
			"tag":        t.Spec.Authentik.Tag,
		}
	}
	if t.Spec.Authentik.LogLevel != "" {
		values.Authentik["log_level"] = t.Spec.Authentik.LogLevel
	}

	targetName := t.Spec.TargetName
	if targetName == "" {
		targetName = t.Name
	}
	// Consumed by the Pyroscope SDK as its server address - always set,
	// unmarked by any enabled flag, matching the reference Ansible harness
	// (env.j2's AUTHENTIK_PYROSCOPE_HOST): profiling is best-effort and
	// silently no-ops if nothing is actually listening there.
	appendEnv := func(w map[string]any, name, value string) {
		env, _ := w["env"].([]map[string]string)
		w["env"] = append(env, map[string]string{valuesKeyName: name, "value": value})
	}
	appendEnv(values.Server, "AUTHENTIK_PYROSCOPE_HOST", pyroscopeHost)
	appendEnv(values.Worker, "AUTHENTIK_PYROSCOPE_HOST", pyroscopeHost)

	if t.Spec.Authentik.ErrorReporting.Enabled {
		values.Authentik["error_reporting"] = map[string]any{
			"enabled": true,
			// deployment.environment carries this topology's target name, so
			// spans from two topologies sharing one Tempo can be told apart.
			"environment": targetName,
		}
		sampleRate := t.Spec.Authentik.ErrorReporting.SampleRate
		if sampleRate == "" {
			sampleRate = "0.1"
		}
		for _, w := range []map[string]any{values.Server, values.Worker} {
			appendEnv(w, "AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT", tracingEndpoint)
			appendEnv(w, "AUTHENTIK_ERROR_REPORTING__SAMPLE_RATE", sampleRate)
			// Only meaningful for the server (the worker doesn't serve
			// HTTP), but harmless to set on both - puts the test name that
			// caused a request on its spans as http.request.header.*.
			appendEnv(w, "OTEL_INSTRUMENTATION_HTTP_CAPTURE_HEADERS_SERVER_REQUEST", "x-benchmark-test,x-benchmark-target")
		}
	}

	if withBlueprintVolume {
		const volumeName = "bench-fixtures-blueprint"
		values.Server["volumes"] = []map[string]any{
			{valuesKeyName: volumeName, "persistentVolumeClaim": map[string]any{"claimName": BlueprintPVCName(t)}},
		}
		values.Server["volumeMounts"] = []map[string]any{
			// Not read-only: AuthentikTopologyReconciler writes the blueprint
			// file into this mount itself (see ApplyBlueprint), rather than
			// the content coming from a mounted ConfigMap - a benchmark-scale
			// fixture set (thousands of synthetic users) can exceed a
			// ConfigMap/Secret's ~1MiB etcd-backed size limit, which a PVC
			// (backed by real disk via the local-path-provisioner every
			// workload cluster gets) has no equivalent of.
			{valuesKeyName: volumeName, "mountPath": BlueprintMountPath},
		}
	}

	applyPlacement := func(w map[string]any) {
		if sel := NodeSelector(t.Spec.Placement); len(sel) > 0 {
			w["nodeSelector"] = sel
		}
		if tol := Tolerations(t.Spec.Placement); len(tol) > 0 {
			w["tolerations"] = tol
		}
		if aff := PackedAffinity(t.Spec.Placement, HelmReleaseName(t)); aff != nil {
			w["affinity"] = aff
		}
	}
	applyPlacement(values.Server)
	applyPlacement(values.Worker)

	switch {
	case embedded && pgConn != nil:
		// The bundled subchart auto-generates its own password unless told
		// otherwise; we pin it to our generated value so authentik's own
		// AUTHENTIK_POSTGRESQL__PASSWORD (below) actually matches it.
		values.Postgres["auth"] = map[string]any{valuesKeyPassword: pgConn.Password}
		values.Authentik["postgresql"] = map[string]any{valuesKeyPassword: pgConn.Password}
	case !embedded && pgConn != nil:
		values.Authentik["postgresql"] = map[string]any{
			"host":            pgConn.Host,
			"port":            pgConn.Port,
			valuesKeyName:     pgConn.Database,
			"user":            pgConn.User,
			valuesKeyPassword: pgConn.Password,
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

// BlueprintMountPath is where the fixtures blueprint PVC (see
// withBlueprintVolume on HelmValues) is mounted into the server pod - must
// stay under authentik's own configured blueprints_dir (verified live:
// "/blueprints"), since ak apply_blueprint rejects any path outside it.
const BlueprintMountPath = "/blueprints/custom"

// BlueprintFileName is the file AuthentikTopologyReconciler.ApplyBlueprint
// writes the blueprint content into, under BlueprintMountPath.
const BlueprintFileName = "bench-fixtures.yaml"

// BlueprintPVCName is deterministic from the topology name, mirroring
// HelmReleaseName, so the operator can look it up without storing it.
func BlueprintPVCName(t *benchv1alpha1.AuthentikTopology) string {
	return t.Name + "-bench-fixtures"
}

// BlueprintPVC is the (small - a benchmark-scale blueprint is at most a few
// MB of YAML) PVC AuthentikTopologyReconciler applies onto the runner
// cluster before installing the chart, so the server pod has somewhere
// writable to receive the fixtures blueprint ApplyBlueprint execs in.
func BlueprintPVC(t *benchv1alpha1.AuthentikTopology) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BlueprintPVCName(t),
			Namespace: t.Spec.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("256Mi")},
			},
		},
	}
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
			// helm-controller's own default (5m) isn't enough on a cold
			// cluster: pulling the authentik/postgresql images and letting
			// postgres finish its first-boot initdb can alone take longer
			// than that, well before authentik itself is even up - and once
			// an install attempt exceeds this and exhausts retries,
			// helm-controller marks the release Stalled permanently (no
			// more automatic retries) even if the workload goes healthy
			// moments later.
			Timeout: &metav1.Duration{Duration: helmInstallTimeout},
			// Explicit, rather than Flux's default "[TargetNamespace-]Name"
			// composition, so ServiceEndpoint's "<release>-server" naming
			// stays correct regardless of what the target namespace is
			// named.
			ReleaseName: HelmReleaseName(t),
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   chartName,
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
// "<release>-authentik-server" in the target namespace, serving HTTP on the
// chart's server.service.servicePortHttp (default 80). Confirmed against a
// live install of chart version 2026.8.0 - the chart's fullname template
// always includes "-authentik-", it isn't just the release name.
func ServiceEndpoint(t *benchv1alpha1.AuthentikTopology) string {
	return fmt.Sprintf("%s-authentik-server.%s.svc.cluster.local:80", HelmReleaseName(t), t.Spec.Namespace)
}

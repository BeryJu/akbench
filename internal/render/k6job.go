package render

import (
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// K6Image is the k6 image every rendered workload runs. Pinned rather than
// left to "latest" so a benchmark run is reproducible. Must be >=0.57.0:
// login.ts uses TypeScript syntax (type-only imports, interfaces, typed
// consts), and k6 only transpiles .ts files natively without extra flags
// from that version on - earlier images fail every run with a script parse
// error before a single request is ever sent.
const K6Image = "grafana/k6:0.57.0"

// ScriptVolumeMountPath, FixturesMountPath and UsersFilePath match the paths
// the ported login.ts (and any future script) expects, mirroring
// roles/runner/templates/compose.yml.j2's volume layout.
const (
	ScriptVolumeMountPath = "/data/tests"
	FixturesMountPath     = "/data/fixtures"
	UsersFilePath         = FixturesMountPath + "/users.json"
)

// K6WorkloadParams carries everything render needs to build a k6 pod for one
// TestCase execution (a Quick Job or a Burn Deployment), so the render
// functions below stay call-site readable instead of chains of positional
// strings.
type K6WorkloadParams struct {
	TestCase   *benchv1alpha1.TestCase
	TargetName string
	// BenchHost/BenchPort are the AuthentikDeployment's in-cluster service
	// endpoint this run drives.
	BenchHost string
	BenchPort int32
	// RemoteWriteURL is the runner cluster's Prometheus remote-write
	// endpoint. Local in M1-M3; the mTLS central receiver from M4 on.
	RemoteWriteURL string
	// ScriptConfigMapName holds the k6 script under key matching its
	// filename (e.g. "login.ts" or "script.ts" for a BYO script).
	ScriptConfigMapName string
	// FixturesSecretName holds the generated users.json fixture.
	FixturesSecretName string
}

// EnvVars builds the exact environment contract k6/scripts/login.ts reads,
// ported unchanged from roles/runner/templates/compose.yml.j2 so the
// resulting metrics carry the same meaning as the Ansible-based harness.
func (p K6WorkloadParams) EnvVars() []corev1.EnvVar {
	tc := p.TestCase
	return []corev1.EnvVar{
		{Name: "TEST_ID", Value: tc.Name},
		{Name: "TARGET", Value: p.TargetName},
		{Name: "PROFILE", Value: k6ProfileValue(tc.Spec.Profile)},
		{Name: "BENCH_HOST", Value: p.BenchHost},
		{Name: "BENCH_PORT", Value: fmt.Sprintf("%d", p.BenchPort)},
		{Name: "VUS", Value: fmt.Sprintf("%d", tc.Spec.VUs)},
		{Name: "ITERATIONS", Value: fmt.Sprintf("%d", tc.Spec.Iterations)},
		{Name: "FLOW", Value: tc.Spec.Flow},
		{Name: "QUICK_DURATION", Value: tc.Spec.QuickDuration},
		{Name: "BURN_DURATION", Value: tc.Spec.BurnDuration},
		{Name: "TRACE_SAMPLE_RATE", Value: tc.Spec.TraceSampleRate},
		{Name: "USERS_FILE", Value: UsersFilePath},
		{Name: "K6_PROMETHEUS_RW_SERVER_URL", Value: p.RemoteWriteURL},
		{Name: "K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM", Value: "true"},
		{Name: "K6_PROMETHEUS_RW_PUSH_INTERVAL", Value: "1s"},
	}
}

func k6ProfileValue(p benchv1alpha1.TestProfile) string {
	if p == benchv1alpha1.ProfileBurn {
		return "burn"
	}
	return "quick"
}

// scriptFileName is the file under ScriptVolumeMountPath k6 is told to run.
func scriptFileName(tc *benchv1alpha1.TestCase) string {
	if tc.Spec.ScriptConfigMapRef != nil {
		return "script.ts"
	}
	return tc.Spec.Script
}

// Args builds the k6 command line, matching the --tag scheme every Grafana
// dashboard splits on: testid, target, profile, plus any extraTags.
func (p K6WorkloadParams) Args() []string {
	tc := p.TestCase
	keys := make([]string, 0, len(tc.Spec.ExtraTags))
	for k := range tc.Spec.ExtraTags {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	const tagFlag = "--tag"

	args := make([]string, 0, 9+2*len(keys)+1)
	args = append(args,
		"run",
		"--out", "experimental-prometheus-rw",
		tagFlag, "testid="+tc.Name,
		tagFlag, "target="+p.TargetName,
		tagFlag, "profile="+k6ProfileValue(tc.Spec.Profile),
	)
	for _, k := range keys {
		args = append(args, tagFlag, fmt.Sprintf("%s=%s", k, tc.Spec.ExtraTags[k]))
	}
	args = append(args, ScriptVolumeMountPath+"/"+scriptFileName(tc))
	return args
}

func (p K6WorkloadParams) volumes() ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{
			Name: "scripts",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: p.ScriptConfigMapName},
				},
			},
		},
		{
			Name: "fixtures",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: p.FixturesSecretName},
			},
		},
	}
	mounts := []corev1.VolumeMount{
		{Name: "scripts", MountPath: ScriptVolumeMountPath, ReadOnly: true},
		{Name: "fixtures", MountPath: FixturesMountPath, ReadOnly: true},
	}
	return volumes, mounts
}

// PodTemplate builds the k6 pod template shared by both the Quick Job and the
// Burn Deployment.
func (p K6WorkloadParams) PodTemplate() corev1.PodTemplateSpec {
	volumes, mounts := p.volumes()
	// The real AuthentikDeployment object name (spec.deploymentRef.name), not
	// TargetName - TargetName is only the target/metric label and can be
	// overridden to something else, but the deployment's own pods are labeled
	// by object name (DeploymentLabels), so the runner's anti-affinity below
	// has to match on that same value to find them.
	deploymentName := p.TestCase.Spec.DeploymentRef.Name
	labels := map[string]string{
		benchv1alpha1.LabelTestCase: p.TestCase.Name,
		benchv1alpha1.LabelProfile:  k6ProfileValue(p.TestCase.Spec.Profile),
		benchv1alpha1.LabelDeployment: deploymentName,
	}
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:         "k6",
					Image:        K6Image,
					Args:         p.Args(),
					Env:          p.EnvVars(),
					VolumeMounts: mounts,
				},
			},
			Volumes: volumes,
			// The load generator must never land on the same node as the
			// deployment it's driving - otherwise it competes with the very
			// system it's measuring for that node's CPU, skewing the
			// numbers it produces. Required, not preferred: a soft
			// preference could still be overridden by the scheduler under
			// pressure.
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						sameDeploymentAntiAffinityTerm(deploymentName),
					},
				},
			},
		},
	}
}

// QuickJob builds the immutable, individually-inspectable Job a TestRun owns
// for a Quick profile execution. backoffLimit is 0: a failed quick run should
// surface as a failed TestRun, not retry silently.
func QuickJob(name, namespace string, p K6WorkloadParams) *batchv1.Job {
	backoffLimit := int32(0)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template:     p.PodTemplate(),
		},
	}
}

// BurnDeployment builds the self-healing, single-replica Deployment a Burn
// TestCase owns directly, mirroring docker-compose's
// `restart: unless-stopped`. Recreate strategy guarantees the old pod is
// fully gone before a config change starts a new one, so a target is never
// hit by two overlapping burn pods.
func BurnDeployment(name, namespace string, p K6WorkloadParams) *appsv1.Deployment {
	replicas := int32(1)
	tmpl := p.PodTemplate()
	tmpl.Spec.RestartPolicy = corev1.RestartPolicyAlways
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{
		benchv1alpha1.LabelTestCase: p.TestCase.Name,
	}}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: selector,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: tmpl,
		},
	}
}

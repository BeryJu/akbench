package api

import (
	"context"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

func TestCreateRun(t *testing.T) {
	s, c := newServer(t, Config{},
		sampleDeployment("single-node"),
		sampleTestCase("login-quick", "single-node", benchv1alpha1.ProfileQuick),
	)

	rec := do(t, s, http.MethodPost, "/api/v1/testcases/login-quick/runs", nil)
	requireStatus(t, rec, http.StatusCreated)

	var runs benchv1alpha1.TestRunList
	if err := c.List(context.Background(), &runs, client.InNamespace(testNamespace)); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs.Items) != 1 {
		t.Fatalf("got %d TestRuns, want 1", len(runs.Items))
	}
	run := runs.Items[0]
	if run.Spec.TestCaseRef.Name != "login-quick" {
		t.Fatalf("run references %q", run.Spec.TestCaseRef.Name)
	}
	// The label is what makes the run show up in the test case's history.
	if got := run.Labels[benchv1alpha1.LabelTestCase]; got != "login-quick" {
		t.Fatalf("run is labelled %q, want login-quick", got)
	}
	if run.GenerateName != "login-quick-" {
		t.Fatalf("run generateName is %q", run.GenerateName)
	}
	if run.Spec.DeploymentRef == nil || run.Spec.DeploymentRef.Name != "single-node" {
		t.Fatalf("run deploymentRef = %+v, want single-node", run.Spec.DeploymentRef)
	}
}

// A TestCase driving more than one deployment fans "Run now" out into one
// TestRun per target, each pinned to its own deployment - the metrics-facing
// motivation for multi-deployment TestCases in the first place (one request,
// N independently inspectable runs instead of N near-identical TestCases).
func TestCreateRunFansOutAcrossMultipleDeployments(t *testing.T) {
	tc := sampleTestCase("login-quick", "single-node", benchv1alpha1.ProfileQuick)
	tc.Spec.DeploymentRefs = []corev1.LocalObjectReference{{Name: "single-node"}, {Name: "multi-node"}}

	s, c := newServer(t, Config{},
		sampleDeployment("single-node"),
		sampleDeployment("multi-node"),
		tc,
	)

	rec := do(t, s, http.MethodPost, "/api/v1/testcases/login-quick/runs", nil)
	requireStatus(t, rec, http.StatusCreated)

	var runs benchv1alpha1.TestRunList
	if err := c.List(context.Background(), &runs, client.InNamespace(testNamespace)); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs.Items) != 2 {
		t.Fatalf("got %d TestRuns, want 2 (one per deployment)", len(runs.Items))
	}
	targets := map[string]bool{}
	for _, run := range runs.Items {
		if run.Spec.DeploymentRef == nil {
			t.Fatalf("run %s has no deploymentRef", run.Name)
		}
		targets[run.Spec.DeploymentRef.Name] = true
	}
	if !targets["single-node"] || !targets["multi-node"] {
		t.Fatalf("runs target %v, want both single-node and multi-node", targets)
	}
}

func TestCreateRunWithOverrides(t *testing.T) {
	s, c := newServer(t, Config{},
		sampleDeployment("single-node"),
		sampleTestCase("login-quick", "single-node", benchv1alpha1.ProfileQuick),
	)

	rec := do(t, s, http.MethodPost, "/api/v1/testcases/login-quick/runs", map[string]any{
		"overrides": map[string]any{"vus": 64, "iterations": 10},
	})
	requireStatus(t, rec, http.StatusCreated)

	var runs benchv1alpha1.TestRunList
	if err := c.List(context.Background(), &runs, client.InNamespace(testNamespace)); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	overrides := runs.Items[0].Spec.Overrides
	if overrides == nil || overrides.VUs == nil || *overrides.VUs != 64 {
		t.Fatalf("overrides did not survive: %+v", overrides)
	}
	if overrides.Iterations == nil || *overrides.Iterations != 10 {
		t.Fatalf("iteration override did not survive: %+v", overrides)
	}
	// An override must not leak back into the TestCase it came from.
	var tc benchv1alpha1.TestCase
	if err := c.Get(context.Background(), client.ObjectKey{Name: "login-quick", Namespace: testNamespace}, &tc); err != nil {
		t.Fatalf("get test case: %v", err)
	}
	if tc.Spec.VUs != 8 {
		t.Fatalf("TestCase was mutated: vus=%d", tc.Spec.VUs)
	}
}

// A Burn test case is already running continuously; asking for a run of one is
// a user error worth an explanation rather than a TestRun that fails later.
func TestCreateRunRejectsBurnProfile(t *testing.T) {
	s, c := newServer(t, Config{},
		sampleDeployment("single-node"),
		sampleTestCase("login-burn", "single-node", benchv1alpha1.ProfileBurn),
	)

	rec := do(t, s, http.MethodPost, "/api/v1/testcases/login-burn/runs", nil)
	requireStatus(t, rec, http.StatusBadRequest)

	var runs benchv1alpha1.TestRunList
	if err := c.List(context.Background(), &runs, client.InNamespace(testNamespace)); err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs.Items) != 0 {
		t.Fatalf("got %d TestRuns, want none", len(runs.Items))
	}
}

func TestCreateRunRejectsArchivedTestCase(t *testing.T) {
	tc := sampleTestCase("login-quick", "single-node", benchv1alpha1.ProfileQuick)
	tc.Spec.Archived = true
	s, _ := newServer(t, Config{}, sampleDeployment("single-node"), tc)

	rec := do(t, s, http.MethodPost, "/api/v1/testcases/login-quick/runs", nil)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestCreateRunForUnknownTestCase(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodPost, "/api/v1/testcases/ghost/runs", nil)
	requireStatus(t, rec, http.StatusNotFound)
}

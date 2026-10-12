package api

import (
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testEvent(name, kind, object, reason string, last time.Time) *corev1.Event {
	return &corev1.Event{
		Name: name, Namespace: testNamespace,
		InvolvedObject: corev1.ObjectReference{Kind: kind, Name: object, Namespace: testNamespace},
		Type:           corev1.EventTypeNormal,
		Reason:         reason,
		Message:        reason + " happened",
		Count:          2,
		FirstTimestamp: metav1.NewTime(last.Add(-time.Minute)),
		LastTimestamp:  metav1.NewTime(last),
		Source:         corev1.EventSource{Component: "authentik-bench"},
	}
}

func TestListObjectEvents(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	s, _ := newServer(t, Config{},
		testEvent("a", "TestCase", "burn", "Ready", now.Add(-10*time.Minute)),
		testEvent("b", "TestCase", "burn", "DeploymentReady", now),
		// Same name, different kind - must not leak in.
		testEvent("c", "AuthentikDeployment", "burn", "HelmReleaseReady", now),
		testEvent("d", "TestCase", "other", "Ready", now),
	)

	rec := do(t, s, http.MethodGet, "/api/v1/testcases/burn/events", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[struct{ Items []ObjectEvent }](t, rec)
	if len(body.Items) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(body.Items), body.Items)
	}
	if body.Items[0].Reason != "DeploymentReady" || body.Items[1].Reason != "Ready" {
		t.Fatalf("events not newest first: %+v", body.Items)
	}
	first := body.Items[0]
	if first.Count != 2 || first.Source != "authentik-bench" || first.LastSeen != now.Format(time.RFC3339) {
		t.Fatalf("unexpected event fields: %+v", first)
	}
}

func TestListObjectEventsEmpty(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/deployments/nothing/events", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[struct{ Items []ObjectEvent }](t, rec)
	if body.Items == nil || len(body.Items) != 0 {
		t.Fatalf("want an empty (non-null) list, got %+v", body.Items)
	}
}

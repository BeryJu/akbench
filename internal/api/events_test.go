package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestEventsTicketIsSingleUse(t *testing.T) {
	st := newEventsState()
	now := time.Now()
	ticket, err := st.mint(now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !st.redeem(ticket, now) {
		t.Fatal("first redeem should succeed")
	}
	if st.redeem(ticket, now) {
		t.Fatal("second redeem of the same ticket should fail")
	}
}

func TestEventsTicketExpires(t *testing.T) {
	st := newEventsState()
	now := time.Now()
	ticket, err := st.mint(now)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if st.redeem(ticket, now.Add(eventsTicketTTL+time.Second)) {
		t.Fatal("redeem after expiry should fail")
	}
}

func TestConnectEventsRejectsBadTicket(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/events/connect?ticket=not-a-real-ticket", nil)
	requireStatus(t, rec, http.StatusUnauthorized)
}

// TestEventsBroadcastsResourceChanges: a real WebSocket client, connected via
// the same ticket-mint-then-connect flow the UI uses, receives a
// ResourceEvent when a TestCase is created on the fake management cluster -
// the same client s.client.Watch is watching.
func TestEventsBroadcastsResourceChanges(t *testing.T) {
	s, c := newServer(t, Config{})
	httpSrv := httptest.NewServer(s.Router())
	defer httpSrv.Close()

	ctx := t.Context()
	s.RunEventWatchers(ctx)

	rec := do(t, s, http.MethodPost, "/api/v1/events/sessions", nil)
	requireStatus(t, rec, http.StatusCreated)
	var session struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/api/v1/events/connect?ticket=" + session.Ticket
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	tc := sampleTestCase("login-live", "single-node", benchv1alpha1.ProfileQuick)
	if err := c.Create(ctx, sampleDeployment("single-node")); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if err := c.Create(ctx, tc); err != nil {
		t.Fatalf("create testcase: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	var ev ResourceEvent
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatalf("read: %v (waiting for a TestCase event)", err)
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("decode event %q: %v", data, err)
		}
		if ev.Kind == "TestCase" && ev.Name == "login-live" {
			break
		}
	}
}

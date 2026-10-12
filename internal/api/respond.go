package api

import (
	"encoding/json"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// errorBody is the single error shape every endpoint returns, so the frontend
// has exactly one thing to render on failure.
type errorBody struct {
	Error string `json:"error"`
}

// expiresAtKey is the one response field every ticket-minting endpoint
// (shell.go, events.go) and the Grafana session endpoint share - the same
// concept (when this credential stops working), always spelled the same way.
const expiresAtKey = "expiresAt"

// writeItems answers a list endpoint with {"items": ...} - the shape every
// GET /api/v1/<kind> (and the ad hoc alerts/shell-contexts lists) shares,
// mirroring what Kubernetes' own List types marshal their slice field as.
func writeItems(w http.ResponseWriter, r *http.Request, items any) {
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already on the wire, so this can only be logged.
		logf.FromContext(r.Context()).Error(err, "Failed to encode response body")
	}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	writeJSON(w, r, status, errorBody{Error: msg})
}

// writeAPIError maps a Kubernetes API error onto the equivalent HTTP status,
// so a missing TestCase surfaces as 404 rather than a blanket 500.
func writeAPIError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case apierrors.IsNotFound(err):
		status = http.StatusNotFound
	case apierrors.IsAlreadyExists(err):
		status = http.StatusConflict
	case apierrors.IsConflict(err):
		status = http.StatusConflict
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		status = http.StatusBadRequest
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		status = http.StatusForbidden
	}
	if status == http.StatusInternalServerError {
		logf.FromContext(r.Context()).Error(err, "Request failed")
	}
	writeError(w, r, status, err.Error())
}

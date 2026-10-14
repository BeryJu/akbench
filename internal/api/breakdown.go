package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// The trace (traces.go) and profile (profiles.go) breakdowns both summarize
// a sample of a test case's targets' telemetry over a trailing window,
// slowly enough that each is cached and computed once for every viewer.

// breakdownWindows are the trailing windows a breakdown can cover.
var breakdownWindows = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
}

const (
	breakdownCacheTTL = 2 * time.Minute
	// breakdownTimeout bounds one breakdown's queries. They run detached
	// from the request that started them, so a reload or a closed tab
	// mid-query still completes it for the next viewer instead of failing
	// it with "context canceled".
	breakdownTimeout = time.Minute
)

type breakdownCache[T any] struct {
	mu      sync.Mutex
	entries map[string]breakdownEntry[T]
	// inflight shares one computation between concurrent requests for the
	// same key.
	inflight singleflight.Group
}

type breakdownEntry[T any] struct {
	value T
	at    time.Time
}

func newBreakdownCache[T any]() *breakdownCache[T] {
	return &breakdownCache[T]{entries: map[string]breakdownEntry[T]{}}
}

// serveBreakdown answers with cache's entry for key, computing it for the
// TestCase testid when missing or stale. compute reports complete=false
// when any of its queries failed: that result is served but not cached, so
// the next request retries rather than serving the error for the whole TTL.
func serveBreakdown[T any](
	s *Server, w http.ResponseWriter, r *http.Request, cache *breakdownCache[T], testid, key string,
	compute func(ctx context.Context, tc *benchv1alpha1.TestCase) (value T, complete bool),
) {
	cache.mu.Lock()
	cached, hit := cache.entries[key]
	cache.mu.Unlock()
	if hit && time.Since(cached.at) < breakdownCacheTTL {
		writeJSON(w, r, http.StatusOK, cached.value)
		return
	}

	var tc benchv1alpha1.TestCase
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: testid, Namespace: s.cfg.Namespace}, &tc); err != nil {
		writeAPIError(w, r, err)
		return
	}
	result := cache.inflight.DoChan(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), breakdownTimeout)
		defer cancel()
		value, complete := compute(ctx, &tc)
		if complete {
			cache.mu.Lock()
			cache.entries[key] = breakdownEntry[T]{value: value, at: time.Now()}
			cache.mu.Unlock()
		}
		return value, nil
	})
	select {
	case res := <-result:
		writeJSON(w, r, http.StatusOK, res.Val.(T))
	case <-r.Context().Done():
		// The client is gone; the computation carries on for the cache.
	}
}

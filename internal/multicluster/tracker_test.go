package multicluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/cluster-api/controllers/clustercache"
)

// fakeCache stubs only GetHealthCheckingState; WarmingUp calls nothing else.
type fakeCache struct {
	clustercache.ClusterCache
	state clustercache.HealthCheckingState
}

func (f fakeCache) GetHealthCheckingState(context.Context, client.ObjectKey) clustercache.HealthCheckingState {
	return f.state
}

func TestWarmingUp(t *testing.T) {
	key := client.ObjectKey{Namespace: "ns", Name: "wc"}
	failed := clustercache.HealthCheckingState{LastProbeTime: time.Now(), ConsecutiveFailures: 1}
	dropped := clustercache.HealthCheckingState{LastProbeTime: time.Now(), LastProbeSuccessTime: time.Now().Add(-time.Minute), ConsecutiveFailures: 5}
	cases := []struct {
		name    string
		tracker *Tracker
		err     error
		want    bool
	}{
		{"not yet probed after start", &Tracker{cache: fakeCache{}, started: time.Now()}, ErrClusterNotConnected, true},
		{"wrapped error", &Tracker{cache: fakeCache{}, started: time.Now()}, errors.Join(errors.New("ctx"), ErrClusterNotConnected), true},
		{"first connect failed", &Tracker{cache: fakeCache{state: failed}, started: time.Now()}, ErrClusterNotConnected, true},
		{"connected once then dropped", &Tracker{cache: fakeCache{state: dropped}, started: time.Now()}, ErrClusterNotConnected, false},
		{"past startup grace", &Tracker{cache: fakeCache{}, started: time.Now().Add(-StartupGrace - time.Second)}, ErrClusterNotConnected, false},
		{"other error", &Tracker{cache: fakeCache{}, started: time.Now()}, errors.New("boom"), false},
		{"nil tracker", nil, ErrClusterNotConnected, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tracker.WarmingUp(context.Background(), key, tc.err); got != tc.want {
				t.Errorf("WarmingUp() = %v, want %v", got, tc.want)
			}
		})
	}
}

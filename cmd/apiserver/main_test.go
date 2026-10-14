package main

import (
	"strings"
	"testing"
)

func TestCheckTokenLength(t *testing.T) {
	if _, err := checkTokenLength("dev-token", "BENCH_API_TOKEN"); err == nil || !strings.Contains(err.Error(), "BENCH_API_TOKEN") {
		t.Errorf("short token: err = %v, want one naming BENCH_API_TOKEN", err)
	}
	long := strings.Repeat("a", 32)
	if got, err := checkTokenLength(long, "BENCH_API_TOKEN"); err != nil || got != long {
		t.Errorf("32-char token = %q, %v; want it accepted", got, err)
	}
}

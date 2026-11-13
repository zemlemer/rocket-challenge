package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestCloseWithin(t *testing.T) {
	if !closeWithin(func() {}, time.Second) {
		t.Error("close that returns at once: reported as not closed")
	}

	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	if closeWithin(func() { <-release }, 100*time.Millisecond) {
		t.Error("blocking close: reported as closed")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed > time.Second {
		t.Errorf("blocking close: waited %s, want about 100ms", elapsed)
	}
}

// Spec processor-runtime "Pool close blocks" and the other branches of the
// shutdown budget.
func TestShutdownBudget(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	const budget = 200 * time.Millisecond

	t.Run("clean", func(t *testing.T) {
		done := make(chan error, 1)
		done <- nil
		closed := false
		if err := shutdown(done, func() { closed = true }, budget, log); err != nil || !closed {
			t.Fatalf("err = %v, pool closed = %v; want nil, true", err, closed)
		}
	})

	t.Run("pool close blocks", func(t *testing.T) {
		done := make(chan error, 1)
		done <- nil
		release := make(chan struct{})
		defer close(release)
		start := time.Now()
		err := shutdown(done, func() { <-release }, budget, log)
		if err == nil || !strings.Contains(err.Error(), "shutdown was not clean") {
			t.Fatalf("err = %v, want shutdown was not clean", err)
		}
		if elapsed := time.Since(start); elapsed < budget || elapsed > budget+500*time.Millisecond {
			t.Errorf("returned after %s, want about the %s budget", elapsed, budget)
		}
	})

	t.Run("batch still running", func(t *testing.T) {
		start := time.Now()
		closeCalled := false
		err := shutdown(make(chan error), func() { closeCalled = true }, budget, log)
		if err == nil || !strings.Contains(err.Error(), "shutdown was not clean") {
			t.Fatalf("err = %v, want shutdown was not clean", err)
		}
		if elapsed := time.Since(start); elapsed < budget || elapsed > budget+500*time.Millisecond {
			t.Errorf("returned after %s, want about the %s budget", elapsed, budget)
		}
		if closeCalled {
			t.Error("pool close started although the batch still holds a connection")
		}
	})

	t.Run("budget shared", func(t *testing.T) {
		done := make(chan error, 1)
		go func() { time.Sleep(budget * 3 / 4); done <- nil }()
		release := make(chan struct{})
		defer close(release)
		start := time.Now()
		_ = shutdown(done, func() { <-release }, budget, log)
		if elapsed := time.Since(start); elapsed > budget+100*time.Millisecond {
			t.Errorf("returned after %s: the pool close got more than the rest of the %s budget", elapsed, budget)
		}
	})
}

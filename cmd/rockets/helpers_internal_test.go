package main

import (
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

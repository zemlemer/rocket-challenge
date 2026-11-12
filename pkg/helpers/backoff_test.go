package helpers_test

import (
	"testing"
	"time"

	"rockets/pkg/helpers"
)

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond,
		800 * time.Millisecond, 1600 * time.Millisecond, 2 * time.Second, 2 * time.Second,
	}
	for attempt, w := range want {
		if got := helpers.Backoff(attempt); got != w {
			t.Errorf("Backoff(%d) = %s, want %s", attempt, got, w)
		}
	}
	if got := helpers.Backoff(1000); got != 2*time.Second {
		t.Errorf("Backoff(1000) = %s, want cap 2s", got)
	}
}

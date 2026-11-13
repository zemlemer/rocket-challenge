package testdb

import (
	"context"
	"testing"
)

// BatchProcessor is the part of the processing repository Drain needs.
type BatchProcessor interface {
	ProcessBatch(ctx context.Context, limit int) (int, error)
}

// Drain processes batches of up to limit entries until nothing is left and
// returns the number processed.
func Drain(t testing.TB, repo BatchProcessor, limit int) int {
	t.Helper()
	total := 0
	for {
		n, err := repo.ProcessBatch(context.Background(), limit)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return total
		}
		total += n
	}
}

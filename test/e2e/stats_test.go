//go:build e2e

package e2e

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

type stats struct {
	AsOf  time.Time `json:"asOf"`
	Inbox struct {
		Received                int64   `json:"received"`
		Pending                 int64   `json:"pending"`
		Applied                 int64   `json:"applied"`
		Quarantined             int64   `json:"quarantined"`
		Failed                  int64   `json:"failed"`
		OldestPendingAt         *string `json:"oldestPendingAt"`
		OldestPendingAgeSeconds float64 `json:"oldestPendingAgeSeconds"`
	} `json:"inbox"`
	Rockets []struct {
		Channel              string `json:"channel"`
		HighestMessageNumber int64  `json:"highestMessageNumber"`
		Received             int64  `json:"received"`
		Missing              int64  `json:"missing"`
		Pending              int64  `json:"pending"`
		Applied              int64  `json:"applied"`
		Quarantined          int64  `json:"quarantined"`
		Failed               int64  `json:"failed"`
		Complete             bool   `json:"complete"`
	} `json:"rockets"`
}

// assertStatsComplete checks GET /stats after a drained simulator run against
// the inbox, and that every replica reports the same (processing-stats
// "Any replica").
func assertStatsComplete(t *testing.T) {
	t.Helper()
	var first stats
	start := time.Now()
	if code := getJSON(t, "/stats", &first); code != http.StatusOK {
		t.Fatalf("GET /stats = %d", code)
	}
	t.Logf("GET /stats over %d inbox entries took %s", first.Inbox.Received, time.Since(start))

	in := first.Inbox
	if in.Received != int64(queryInt(t, `select count(*) from inbox`)) || in.Pending != 0 || in.Failed != 0 ||
		in.OldestPendingAt != nil || in.Received != in.Applied+in.Quarantined {
		t.Errorf("inbox stats = %+v", in)
	}
	if want := queryInt(t, `select count(distinct channel) from inbox`); len(first.Rockets) != want {
		t.Errorf("%d rockets in stats, %d channels in the inbox", len(first.Rockets), want)
	}
	for _, r := range first.Rockets {
		if !r.Complete || r.Missing != 0 || r.Received != r.HighestMessageNumber {
			t.Errorf("rocket %s incomplete: %+v", r.Channel, r)
		}
	}

	for range 6 { // round-robin reaches every replica twice
		var again stats
		if code := getJSON(t, "/stats", &again); code != http.StatusOK {
			t.Fatalf("GET /stats = %d", code)
		}
		again.AsOf = first.AsOf
		if !reflect.DeepEqual(again, first) {
			t.Fatalf("replicas disagree:\n%+v\n%+v", first, again)
		}
	}
}

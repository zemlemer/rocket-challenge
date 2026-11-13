package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rockets/internal/handler"
	"rockets/internal/repository/intake"
	"rockets/internal/testdb"
)

// Identity edge cases go through the real handler and the real database:
// whatever the body, the answer is 202 and the stored identity is exactly the
// one the extraction rules allow — never an insert error that would make the
// sender redeliver forever.
func TestIntakeIdentityEdgeCasesAgainstDatabase(t *testing.T) {
	_, pool := testdb.Migrated(t)
	messages := &handler.Messages{Repo: intake.NewInbox(pool), Log: testdb.Logger()}

	body := func(channel, number string) string {
		return `{"metadata":{"channel":` + channel + `,"messageNumber":` + number + `},"message":{}}`
	}
	ch255 := strings.Repeat("a", 255)

	tests := []struct {
		name        string
		body        string
		wantChannel string // "" = stored without identity
		wantNumber  int64
	}{
		{"255-byte channel fits the index", body(`"`+ch255+`"`, `1`), ch255, 1},
		{"max int64 number fits bigint", body(`"max"`, `9223372036854775807`), "max", 9223372036854775807},
		{"NUL in channel", body(`"a\u0000b"`, `1`), "", 0},
		{"256-byte channel", body(`"`+strings.Repeat("b", 256)+`"`, `1`), "", 0},
		{"invalid UTF-8 in channel", body("\"bad\xff\xfeutf\"", `2`), "bad��utf", 2},
		{"invalid UTF-8 outside identity", body(`"utf"`, `3`)[:len(body(`"utf"`, `3`))-2] + "\xff}}", "", 0},
		{"binary garbage", "\x00\x01\x02\xff\xfe", "", 0},
		{"empty body", "", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(context.Background(), "truncate inbox"); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			messages.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(tt.body)))
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body)
			}

			var channel *string
			var number *int64
			var stored []byte
			err := pool.QueryRow(context.Background(),
				"select channel, message_number, body from inbox").Scan(&channel, &number, &stored)
			if err != nil {
				t.Fatalf("expected exactly one stored entry: %v", err)
			}
			if string(stored) != tt.body {
				t.Fatalf("body stored as %q, want the raw request body", stored)
			}
			if tt.wantChannel == "" {
				if channel != nil || number != nil {
					t.Fatalf("stored identity (%v, %v), want none", *channel, *number)
				}
				return
			}
			if channel == nil || *channel != tt.wantChannel || *number != tt.wantNumber {
				t.Fatalf("stored identity (%v, %v), want (%q, %d)", channel, number, tt.wantChannel, tt.wantNumber)
			}
		})
	}
}

// A real simulator message goes through the handler into the database with
// all four metadata fields and its body unchanged.
func TestIntakeStoresFullMetadata(t *testing.T) {
	_, pool := testdb.Migrated(t)
	const body = `{"metadata":{"channel":"193270a9-c9cf-404a-8f83-838e71d9ae67","messageNumber":1,` +
		`"messageTime":"2022-02-02T19:39:05.86337+01:00","messageType":"RocketLaunched"},` +
		`"message":{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS"}}`
	rec := httptest.NewRecorder()
	(&handler.Messages{Repo: intake.NewInbox(pool), Log: testdb.Logger()}).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}

	var channel, msgType, timeUTC string
	var number int64
	var stored []byte
	err := pool.QueryRow(context.Background(), `
		select channel, message_number, message_type,
		       to_char(message_time at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'), body
		from inbox`).Scan(&channel, &number, &msgType, &timeUTC, &stored)
	if err != nil {
		t.Fatal(err)
	}
	if channel != "193270a9-c9cf-404a-8f83-838e71d9ae67" || number != 1 || msgType != "RocketLaunched" ||
		timeUTC != "2022-02-02T18:39:05.863370" || string(stored) != body {
		t.Fatalf("stored (%s, %d, %s, %s), body %q", channel, number, msgType, timeUTC, stored)
	}
}

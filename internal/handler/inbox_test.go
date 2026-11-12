package handler_test

import (
	"strings"
	"testing"
	"time"

	"rockets/internal/handler"
)

func TestExtractIdentity(t *testing.T) {
	ch255 := `"` + strings.Repeat("a", 255) + `"`
	ch256 := `"` + strings.Repeat("a", 256) + `"`
	valid := msg(`"193270a9-c9cf-404a-8f83-838e71d9ae67"`, `1`)

	tests := []struct {
		name    string
		body    string
		channel string // "" = no identity expected
		number  int64
	}{
		{"valid", valid, "193270a9-c9cf-404a-8f83-838e71d9ae67", 1},
		{"large number", msg(`"c"`, `9223372036854775807`), "c", 9223372036854775807},
		{"255-byte channel", msg(ch255, `2`), strings.Repeat("a", 255), 2},
		{"duplicate keys: last wins", `{"metadata":{"channel":"a","channel":"b","messageNumber":3}}`, "b", 3},
		{"missing metadata", `{"message":{}}`, "", 0},
		{"missing channel", `{"metadata":{"messageNumber":1}}`, "", 0},
		{"missing number", `{"metadata":{"channel":"c"}}`, "", 0},
		{"empty channel", msg(`""`, `1`), "", 0},
		{"non-string channel", msg(`42`, `1`), "", 0},
		{"NUL in channel", msg(`"a\u0000b"`, `1`), "", 0},
		{"256-byte channel", msg(ch256, `1`), "", 0},
		{"fraction", msg(`"c"`, `1.5`), "", 0},
		{"exponent", msg(`"c"`, `1e3`), "", 0},
		{"string number", msg(`"c"`, `"1"`), "", 0},
		{"zero", msg(`"c"`, `0`), "", 0},
		{"negative", msg(`"c"`, `-3`), "", 0},
		{"leading zero", msg(`"c"`, `01`), "", 0},
		{"int64 overflow", msg(`"c"`, `9223372036854775808`), "", 0},
		{"null number", msg(`"c"`, `null`), "", 0},
		{"metadata not an object", `{"metadata":[1,2]}`, "", 0},
		{"top-level array", `[1,2]`, "", 0},
		{"invalid JSON", `{"metadata":`, "", 0},
		{"truncated", valid[:len(valid)-10], "", 0},
		{"empty body", ``, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.ExtractMetadata([]byte(tt.body)).Identity
			if tt.channel == "" {
				if got != nil {
					t.Fatalf("got identity %+v, want none", *got)
				}
				return
			}
			if got == nil || got.Channel != tt.channel || got.MessageNumber != tt.number {
				t.Fatalf("got %+v, want (%q, %d)", got, tt.channel, tt.number)
			}
		})
	}
}

func TestExtractMessageTime(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        string // RFC 3339 in UTC, "" = none
	}{
		{"offset, 5 fraction digits", `"2022-02-02T19:39:05.86337+01:00"`, "2022-02-02T18:39:05.86337Z"},
		{"Z, 6 fraction digits", `"2026-11-12T19:16:03.853369Z"`, "2026-11-12T19:16:03.853369Z"},
		{"no fraction", `"2022-02-02T19:39:05-03:30"`, "2022-02-02T23:09:05Z"},
		{"2 fraction digits", `"2026-11-12T19:16:03.85+01:00"`, "2026-11-12T18:16:03.85Z"},
		{"no zone", `"2022-02-02T19:39:05"`, ""},
		{"date only", `"2022-02-02"`, ""},
		{"free text", `"yesterday"`, ""},
		{"number", `1643827145`, ""},
		{"null", `null`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.ExtractMetadata([]byte(meta(`"messageTime":` + tc.value))).MessageTime
			if tc.want == "" {
				if got != nil {
					t.Fatalf("got %v, want none", *got)
				}
				return
			}
			if got == nil || got.UTC().Format(time.RFC3339Nano) != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}

func TestExtractMessageType(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"known type", `"RocketLaunched"`, "RocketLaunched"},
		{"unknown type is kept", `"RocketTeleported"`, "RocketTeleported"},
		{"255 bytes", `"` + strings.Repeat("t", 255) + `"`, strings.Repeat("t", 255)},
		{"256 bytes", `"` + strings.Repeat("t", 256) + `"`, ""},
		{"empty", `""`, ""},
		{"NUL", `"Rocket\u0000Launched"`, ""},
		{"not a string", `42`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.ExtractMetadata([]byte(meta(`"messageType":` + tc.value))).MessageType
			if tc.want == "" {
				if got != nil {
					t.Fatalf("got %q, want none", *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				t.Fatalf("got %v, want %q", got, tc.want)
			}
		})
	}
}

// Each field is kept or dropped on its own.
func TestMetadataFieldsAreIndependent(t *testing.T) {
	bad := handler.ExtractMetadata([]byte(meta(`"channel":"c1","messageNumber":3,"messageTime":"yesterday","messageType":"RocketExploded"`)))
	if bad.Identity == nil || bad.MessageType == nil || bad.MessageTime != nil {
		t.Fatalf("malformed time must not hide identity and type: %+v", bad)
	}
	noID := handler.ExtractMetadata([]byte(meta(`"channel":"c1","messageNumber":1.5,"messageTime":"2022-02-02T19:39:05Z","messageType":"RocketExploded"`)))
	if noID.Identity != nil || noID.MessageTime == nil || noID.MessageType == nil {
		t.Fatalf("malformed number must only drop the identity: %+v", noID)
	}
	if md := handler.ExtractMetadata(nil); md.Identity != nil || md.MessageTime != nil || md.MessageType != nil {
		t.Fatalf("empty body must give no metadata: %+v", md)
	}
}

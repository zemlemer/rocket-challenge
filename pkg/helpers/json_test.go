package helpers_test

import (
	"encoding/json"
	"strings"
	"testing"

	"rockets/pkg/helpers"
)

func TestBoundedString(t *testing.T) {
	for raw, ok := range map[string]bool{
		`"a"`: true, `"` + strings.Repeat("x", 255) + `"`: true,
		`""`: false, `"` + strings.Repeat("x", 256) + `"`: false, `"a\u0000b"`: false,
		`1`: false, `null`: false, ``: false,
	} {
		if _, got := helpers.BoundedString(json.RawMessage(raw)); got != ok {
			t.Errorf("BoundedString(%.20q) ok=%v, want %v", raw, got, ok)
		}
	}
}

func TestNonNegativeInt(t *testing.T) {
	for raw, want := range map[string]int64{
		`0`: 0, `7`: 7, ` 42 `: 42, `9223372036854775807`: 9223372036854775807,
	} {
		if n, ok := helpers.NonNegativeInt(json.RawMessage(raw)); !ok || n != want {
			t.Errorf("NonNegativeInt(%q) = %d,%v", raw, n, ok)
		}
	}
	for _, raw := range []string{`-1`, `01`, `1.5`, `1e3`, `"1"`, `null`, ``, `9223372036854775808`} {
		if _, ok := helpers.NonNegativeInt(json.RawMessage(raw)); ok {
			t.Errorf("NonNegativeInt(%q) accepted", raw)
		}
	}
}

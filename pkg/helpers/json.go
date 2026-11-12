package helpers

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// MaxStringBytes bounds strings taken from messages: they are stored in
// PostgreSQL text columns and some are indexed (btree entries are limited).
const MaxStringBytes = 255

var nonNegativeLiteral = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

// BoundedString accepts a non-empty JSON string of at most MaxStringBytes
// without NUL (PostgreSQL text cannot hold NUL).
func BoundedString(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" || len(s) > MaxStringBytes || strings.ContainsRune(s, 0) {
		return "", false
	}
	return s, true
}

// NonNegativeInt accepts a JSON integer literal — no sign, fraction or
// exponent, no leading zeros — that fits an int64.
func NonNegativeInt(raw json.RawMessage) (int64, bool) {
	tok := string(bytes.TrimSpace(raw))
	if !nonNegativeLiteral.MatchString(tok) {
		return 0, false
	}
	n, err := strconv.ParseInt(tok, 10, 64)
	return n, err == nil
}

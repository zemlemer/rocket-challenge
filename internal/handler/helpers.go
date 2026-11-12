package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxStringBytes bounds channel and messageType so they fit a btree index
// entry and stay reasonable.
const maxStringBytes = 255

var integerLiteral = regexp.MustCompile(`^[1-9][0-9]*$`)

// readBounded reads up to limit bytes; truncated reports that more followed.
// Any read error before the end of the body or the limit is returned.
func readBounded(r io.Reader, limit int) (body []byte, truncated bool, err error) {
	body, err = io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return body, false, err
	}
	if len(body) > limit {
		return body[:limit], true, nil
	}
	return body, false, nil
}

// boundedString accepts a non-empty JSON string of at most maxStringBytes
// without NUL (PostgreSQL text cannot hold NUL).
func boundedString(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" || len(s) > maxStringBytes || strings.ContainsRune(s, 0) {
		return "", false
	}
	return s, true
}

// positiveInt64 accepts a JSON integer literal (no sign, fraction or
// exponent) between 1 and 2^63-1.
func positiveInt64(raw json.RawMessage) (int64, bool) {
	tok := string(bytes.TrimSpace(raw))
	if !integerLiteral.MatchString(tok) {
		return 0, false
	}
	n, err := strconv.ParseInt(tok, 10, 64)
	return n, err == nil
}

// rfc3339 accepts a JSON string holding an RFC 3339 timestamp with any number
// of fractional digits and a zone (Z or numeric offset).
func rfc3339(raw json.RawMessage) (time.Time, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

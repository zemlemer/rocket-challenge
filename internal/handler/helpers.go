package handler

import (
	"encoding/json"
	"io"
	"time"
)

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

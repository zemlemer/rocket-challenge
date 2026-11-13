package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"rockets/pkg/helpers"
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

// readFailed answers a failed read: nothing for a client that has gone, 503
// when storage is unavailable or too slow, 500 otherwise.
func readFailed(w http.ResponseWriter, r *http.Request, log *slog.Logger, op string, err error) {
	switch {
	case r.Context().Err() != nil:
		log.Debug(op+": client gone", "error", err)
	case errors.Is(err, ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		log.Error(op+": storage unavailable", "error", err)
		helpers.WriteError(w, http.StatusServiceUnavailable, "storage unavailable, retry later")
	default:
		log.Error(op+" failed", "error", err)
		helpers.WriteError(w, http.StatusInternalServerError, "rockets could not be read")
	}
}

// parseRocketQuery reads sort and order from a raw query string. It returns
// an error message for the client when the query is not acceptable: the
// string is malformed, or sort/order is unknown, empty or repeated.
func parseRocketQuery(raw string) (RocketQuery, string) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return RocketQuery{}, "query string is malformed: " + err.Error()
	}
	q := RocketQuery{Sort: SortChannel, Order: Asc}

	if v, ok := values["sort"]; ok {
		if len(v) != 1 || !slices.Contains(SortFields, SortField(v[0])) {
			return RocketQuery{}, "sort: must be given once as one of channel, type, mission, speed, status, launchedAt"
		}
		q.Sort = SortField(v[0])
	}
	if v, ok := values["order"]; ok {
		if len(v) != 1 || (v[0] != string(Asc) && v[0] != string(Desc)) {
			return RocketQuery{}, "order: must be given once as asc or desc"
		}
		q.Order = SortOrder(v[0])
	}
	return q, ""
}

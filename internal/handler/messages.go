package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"rockets/pkg/helpers"
	"time"
)

// MaxBodyBytes is the largest body stored in the inbox.
const MaxBodyBytes = 1 << 20

// saveTimeout bounds a single inbox insert.
const saveTimeout = 10 * time.Second

// Messages handles POST /messages: store first, understand later. It answers
// 202 once the request is stored and 5xx otherwise — never 4xx, because
// senders redeliver a message unchanged until they get a 2xx.
type Messages struct {
	Repo InboxRepository
	Log  *slog.Logger
	// Work is the server-lifetime context inserts run on. It is cancelled
	// only when the server gives up on in-flight work; nil means
	// context.Background().
	Work context.Context
}

func (h *Messages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, truncated, err := readBounded(r.Body, MaxBodyBytes)
	if err != nil {
		h.Log.Warn("request body could not be read completely; nothing stored", "error", err)
		helpers.WriteError(w, http.StatusInternalServerError, "request body could not be read completely")
		return
	}

	entry := InboxEntry{Body: body, Truncated: truncated}
	if !truncated {
		entry.Metadata = ExtractMetadata(body)
	}
	if truncated {
		h.Log.Warn("storing entry for oversized request body", "limit_bytes", MaxBodyBytes)
	} else if entry.Identity == nil {
		h.Log.Warn("storing entry without identity", "bytes", len(body))
	}

	// Detach from the client: once the body is read, finishing the insert is
	// harmless (identity dedup) and avoids an ambiguous half-done save. Only
	// the server ends it, when its shutdown timeout expires.
	work := h.Work
	if work == nil {
		work = context.Background()
	}
	ctx, cancel := context.WithTimeout(work, saveTimeout)
	defer cancel()
	if err := h.Repo.Save(ctx, entry); err != nil {
		if errors.Is(err, ErrUnavailable) {
			h.Log.Error("inbox unavailable", "error", err)
			helpers.WriteError(w, http.StatusServiceUnavailable, "storage unavailable, retry later")
			return
		}
		h.Log.Error("inbox save failed", "error", err)
		helpers.WriteError(w, http.StatusInternalServerError, "message could not be stored")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

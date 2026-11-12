// Package handler contains the HTTP handlers of the rockets server. It owns
// the repository interfaces it needs; storage implementations live elsewhere
// and depend on this package, never the other way round.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Identity identifies a message: its channel (the rocket) and its number
// within that channel.
type Identity struct {
	Channel       string
	MessageNumber int64
}

// Metadata is the parsed "metadata" object of a message. Each field is nil
// when it was missing or malformed; Identity needs both channel and number.
type Metadata struct {
	Identity    *Identity
	MessageTime *time.Time
	MessageType *string
}

// InboxEntry is one accepted request as it is stored: the body unchanged plus
// the metadata parsed from it.
type InboxEntry struct {
	Metadata
	Body      []byte
	Truncated bool
}

// InboxRepository is what the Messages handler needs from storage.
type InboxRepository interface {
	// Save stores the entry. An entry whose identity is already stored is
	// not stored again and Save returns nil.
	Save(ctx context.Context, e InboxEntry) error
}

// ErrUnavailable marks storage failures worth a 503: the sender should retry.
var ErrUnavailable = errors.New("storage unavailable")

// ExtractMetadata parses the "metadata" object of body. It never fails: each
// field is kept when well-formed and left nil otherwise, independently of the
// others. The "message" payload is not decoded.
func ExtractMetadata(body []byte) Metadata {
	var probe struct {
		Metadata struct {
			Channel       json.RawMessage `json:"channel"`
			MessageNumber json.RawMessage `json:"messageNumber"`
			MessageTime   json.RawMessage `json:"messageTime"`
			MessageType   json.RawMessage `json:"messageType"`
		} `json:"metadata"`
	}
	if len(body) == 0 || json.Unmarshal(body, &probe) != nil {
		return Metadata{}
	}
	m := probe.Metadata

	var md Metadata
	channel, okChannel := boundedString(m.Channel)
	number, okNumber := positiveInt64(m.MessageNumber)
	if okChannel && okNumber {
		md.Identity = &Identity{Channel: channel, MessageNumber: number}
	}
	if t, ok := rfc3339(m.MessageTime); ok {
		md.MessageTime = &t
	}
	if typ, ok := boundedString(m.MessageType); ok {
		md.MessageType = &typ
	}
	return md
}

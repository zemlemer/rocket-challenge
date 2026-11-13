package domain

import (
	"bytes"
	"encoding/json"
)

// Classify returns the conforming Message held by e, or a *Rejection naming
// the first rule it breaks. Metadata comes from the inbox columns; only the
// "message" object of the body is decoded, for the entry's type.
func Classify(e InboxEntry) (Message, error) {
	switch {
	case e.Truncated:
		return Message{}, reject("body: exceeds the 1 MiB size limit")
	case e.Channel == nil || e.MessageNumber == nil:
		return Message{}, reject("metadata: channel and messageNumber required")
	case !ValidChannel(*e.Channel):
		return Message{}, reject("metadata.channel: must contain only ASCII letters, digits, '-' or '_' (1 to 255 bytes)")
	case e.MessageTime == nil:
		return Message{}, reject("metadata.messageTime: required")
	case e.MessageType == nil:
		return Message{}, reject("metadata.messageType: required")
	}
	m := Message{Channel: *e.Channel, Number: *e.MessageNumber, Time: *e.MessageTime, Type: *e.MessageType}

	fields, ok := knownTypes[m.Type]
	if !ok {
		return Message{}, reject("metadata.messageType: unknown type %q", m.Type)
	}
	if m.Number == 1 && m.Type != TypeLaunched {
		return Message{}, reject("message number 1 must be a %s, got %s", TypeLaunched, m.Type)
	}
	if m.Type == TypeLaunched && m.Number != 1 {
		return Message{}, reject("%s must be message number 1, got %d", TypeLaunched, m.Number)
	}

	payload, err := messageObject(e.Body)
	if err != nil {
		return Message{}, err
	}
	for _, f := range fields {
		raw, present := payload[f.name]
		if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Message{}, reject("message.%s: required", f.name)
		}
		if err := f.set(&m, raw); err != nil {
			return Message{}, err
		}
	}
	return m, nil
}

// messageObject decodes the body's "message" member, which must be an object.
func messageObject(body []byte) (map[string]json.RawMessage, error) {
	var env struct {
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(body, &env) != nil {
		return nil, reject("body: invalid JSON")
	}
	raw := bytes.TrimSpace(env.Message)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, reject("message: must be an object")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, reject("message: must be an object")
	}
	return fields, nil
}

type field struct {
	name string
	set  func(*Message, json.RawMessage) error
}

var knownTypes = map[string][]field{
	TypeLaunched: {
		stringField("type", func(m *Message, s string) { m.RocketType = s }),
		amountField("launchSpeed", func(m *Message, n int64) { m.LaunchSpeed = n }),
		stringField("mission", func(m *Message, s string) { m.Mission = s }),
	},
	TypeSpeedIncreased: {amountField("by", func(m *Message, n int64) { m.By = n })},
	TypeSpeedDecreased: {amountField("by", func(m *Message, n int64) { m.By = n })},
	TypeExploded:       {stringField("reason", func(m *Message, s string) { m.Reason = s })},
	TypeMissionChanged: {stringField("newMission", func(m *Message, s string) { m.Mission = s })},
}

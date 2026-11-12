package domain

import (
	"encoding/json"
	"fmt"

	"rockets/pkg/helpers"
)

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// stringField reads a non-empty string of at most 255 bytes without NUL.
func stringField(name string, set func(*Message, string)) field {
	return field{name, func(m *Message, raw json.RawMessage) error {
		s, ok := helpers.BoundedString(raw)
		if !ok {
			return reject("message.%s: must be a non-empty string of at most %d bytes without NUL", name, helpers.MaxStringBytes)
		}
		set(m, s)
		return nil
	}}
}

// amountField reads an integer literal from 0 to MaxAmount.
func amountField(name string, set func(*Message, int64)) field {
	return field{name, func(m *Message, raw json.RawMessage) error {
		n, ok := helpers.NonNegativeInt(raw)
		if !ok || n > MaxAmount {
			return reject("message.%s: must be an integer from 0 to %d", name, MaxAmount)
		}
		set(m, n)
		return nil
	}}
}

func higherMission(a, b *MissionValue) *MissionValue {
	if a == nil || (b != nil && b.Number > a.Number) {
		return b
	}
	return a
}

func laterStamp(a, b *Stamp) *Stamp {
	if a == nil || (b != nil && b.Number > a.Number) {
		return b
	}
	return a
}

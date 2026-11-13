package domain

import "rockets/pkg/helpers"

// ValidChannel reports whether channel satisfies the channel rule: 1 to 255
// bytes of ASCII letters, digits, '-' and '_'. Processing quarantines
// messages on other channels and the query API hides rockets with them, so
// both use this one function.
func ValidChannel(channel string) bool {
	if len(channel) == 0 || len(channel) > helpers.MaxStringBytes {
		return false
	}
	for i := 0; i < len(channel); i++ {
		c := channel[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z',
			c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

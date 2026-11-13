package domain_test

import (
	"strings"
	"testing"

	"rockets/internal/domain"
)

func TestValidChannel(t *testing.T) {
	for channel, want := range map[string]bool{
		"193270a9-c9cf-404a-8f83-838e71d9ae67": true,
		"Rocket_7-Az09":                        true,
		strings.Repeat("a", 255):               true,
		"":                                     false,
		strings.Repeat("a", 256):               false,
		"a.b":                                  false,
		"a/b":                                  false,
		"a%2Fb":                                false,
		"a b":                                  false,
		"a\n":                                  false,
		"é":                                    false,
		"ß":                                    false,
		"€":                                    false,
		".":                                    false,
		"..":                                   false,
	} {
		if got := domain.ValidChannel(channel); got != want {
			t.Errorf("ValidChannel(%q) = %v, want %v", channel, got, want)
		}
	}
}

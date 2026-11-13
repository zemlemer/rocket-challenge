package domain_test

import (
	"errors"
	"strings"
	"testing"

	"rockets/internal/domain"
)

func TestClassifyConforming(t *testing.T) {
	for name, tc := range map[string]struct {
		e    domain.InboxEntry
		want domain.Message
	}{
		"launched": {entry(1, "c", 1, domain.TypeLaunched, `{"type":"Falcon-9","launchSpeed":500,"mission":"ARTEMIS","extra":true}`),
			domain.Message{Type: domain.TypeLaunched, RocketType: "Falcon-9", LaunchSpeed: 500, Mission: "ARTEMIS"}},
		"increased":  {entry(1, "c", 2, domain.TypeSpeedIncreased, `{"by":3000}`), domain.Message{Type: domain.TypeSpeedIncreased, By: 3000}},
		"decreased":  {entry(1, "c", 3, domain.TypeSpeedDecreased, `{"by":0}`), domain.Message{Type: domain.TypeSpeedDecreased, By: 0}},
		"exploded":   {entry(1, "c", 4, domain.TypeExploded, `{"reason":"PRESSURE_VESSEL_FAILURE"}`), domain.Message{Type: domain.TypeExploded, Reason: "PRESSURE_VESSEL_FAILURE"}},
		"mission":    {entry(1, "c", 5, domain.TypeMissionChanged, `{"newMission":"SHUTTLE_MIR"}`), domain.Message{Type: domain.TypeMissionChanged, Mission: "SHUTTLE_MIR"}},
		"max amount": {entry(1, "c", 6, domain.TypeSpeedIncreased, `{"by":1000000000}`), domain.Message{Type: domain.TypeSpeedIncreased, By: 1_000_000_000}},
		"255 bytes":  {entry(1, "c", 7, domain.TypeMissionChanged, `{"newMission":"`+strings.Repeat("m", 255)+`"}`), domain.Message{Type: domain.TypeMissionChanged, Mission: strings.Repeat("m", 255)}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := domain.Classify(tc.e)
			if err != nil {
				t.Fatalf("rejected: %v", err)
			}
			tc.want.Channel, tc.want.Number, tc.want.Time = *tc.e.Channel, *tc.e.MessageNumber, *tc.e.MessageTime
			if got != tc.want {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestClassifyChannelAlphabet(t *testing.T) {
	for _, tc := range []struct {
		name, channel string
		valid         bool
	}{
		{"mixed ASCII", "Rocket_7-Az09", true},
		{"hyphens only", "---", true},
		{"underscores only", "___", true},
		{"255 bytes", strings.Repeat("A", 255), true},
		{"empty", "", false},
		{"256 bytes", strings.Repeat("A", 256), false},
		{"slash", "a/b", false},
		{"dot", "a.b", false},
		{"percent", "a%b", false},
		{"question mark", "a?b", false},
		{"fragment mark", "a#b", false},
		{"space", "a b", false},
		{"Unicode", "aé", false},
		{"mixed invalid", "A_7/b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.Classify(entry(1, tc.channel, 1, domain.TypeLaunched,
				`{"type":"A","launchSpeed":1,"mission":"M"}`))
			if tc.valid && err != nil {
				t.Fatalf("valid channel %q rejected: %v", tc.channel, err)
			}
			if !tc.valid {
				var rej *domain.Rejection
				if !errors.As(err, &rej) || !strings.Contains(rej.Reason, "metadata.channel") {
					t.Fatalf("invalid channel %q: got %v, want channel rejection", tc.channel, err)
				}
			}
		})
	}
}

func TestClassifyRejects(t *testing.T) {
	inc := func(payload string) domain.InboxEntry { return entry(1, "c", 2, domain.TypeSpeedIncreased, payload) }
	noTime := entry(1, "c", 2, domain.TypeSpeedIncreased, `{"by":1}`)
	noTime.MessageTime = nil
	noType := entry(1, "c", 2, domain.TypeSpeedIncreased, `{"by":1}`)
	noType.MessageType = nil
	noID := entry(1, "c", 2, domain.TypeSpeedIncreased, `{"by":1}`)
	noID.Channel, noID.MessageNumber = nil, nil
	truncated := entry(1, "c", 2, domain.TypeSpeedIncreased, `{"by":1}`)
	truncated.Truncated = true
	invalidJSON := entry(1, "c", 2, domain.TypeSpeedIncreased, `{"by":1}`)
	invalidJSON.Body = []byte(`{"metadata":`)
	noMessage := entry(1, "c", 2, domain.TypeSpeedIncreased, `{}`)
	noMessage.Body = []byte(`{"metadata":{}}`)
	long := strings.Repeat("x", 256)

	cases := map[string]struct {
		e      domain.InboxEntry
		reason string
	}{
		"historical truncated":   {truncated, "body: exceeds the 1 MiB size limit"},
		"oversized without body": {domain.InboxEntry{Truncated: true}, "body: exceeds the 1 MiB size limit"},
		"no identity":            {noID, "metadata: channel and messageNumber required"},
		"no time":                {noTime, "metadata.messageTime: required"},
		"no type":                {noType, "metadata.messageType: required"},
		"unknown type":           {entry(1, "c", 2, "RocketTeleported", `{}`), `unknown type "RocketTeleported"`},
		"launch not number 1":    {entry(1, "c", 6, domain.TypeLaunched, `{"type":"A","launchSpeed":1,"mission":"M"}`), "RocketLaunched must be message number 1"},
		"number 1 not a launch":  {entry(1, "c", 1, domain.TypeSpeedIncreased, `{"by":1}`), "message number 1 must be a RocketLaunched"},
		"invalid JSON":           {invalidJSON, "body: invalid JSON"},
		"no message":             {noMessage, "message: must be an object"},
		"message null":           {inc(`null`), "message: must be an object"},
		"message array":          {inc(`[]`), "message: must be an object"},
		"message string":         {inc(`"x"`), "message: must be an object"},
		"message number":         {inc(`5`), "message: must be an object"},
		"by missing":             {inc(`{}`), "message.by: required"},
		"by null":                {inc(`{"by":null}`), "message.by: required"},
		"by negative":            {inc(`{"by":-5}`), "message.by: must be an integer from 0 to 1000000000"},
		"by above max":           {inc(`{"by":1000000001}`), "message.by: must be an integer"},
		"by fraction":            {inc(`{"by":1.5}`), "message.by: must be an integer"},
		"by exponent":            {inc(`{"by":1e3}`), "message.by: must be an integer"},
		"by string":              {inc(`{"by":"5"}`), "message.by: must be an integer"},
		"decreased missing":      {entry(1, "c", 2, domain.TypeSpeedDecreased, `{}`), "message.by: required"},
		"launch empty":           {entry(1, "c", 1, domain.TypeLaunched, `{}`), "message.type: required"},
		"launch speed fraction":  {entry(1, "c", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":500.5,"mission":"M"}`), "message.launchSpeed: must be an integer"},
		"launch type empty":      {entry(1, "c", 1, domain.TypeLaunched, `{"type":"","launchSpeed":1,"mission":"M"}`), "message.type: must be a non-empty string"},
		"launch mission missing": {entry(1, "c", 1, domain.TypeLaunched, `{"type":"A","launchSpeed":1}`), "message.mission: required"},
		"exploded empty":         {entry(1, "c", 3, domain.TypeExploded, `{}`), "message.reason: required"},
		"exploded empty reason":  {entry(1, "c", 3, domain.TypeExploded, `{"reason":""}`), "message.reason: must be a non-empty string"},
		"mission empty":          {entry(1, "c", 3, domain.TypeMissionChanged, `{}`), "message.newMission: required"},
		"mission NUL":            {entry(1, "c", 3, domain.TypeMissionChanged, `{"newMission":"a\u0000b"}`), "message.newMission: must be a non-empty string of at most 255 bytes without NUL"},
		"mission 256 bytes":      {entry(1, "c", 3, domain.TypeMissionChanged, `{"newMission":"`+long+`"}`), "message.newMission: must be a non-empty string"},
		"mission not string":     {entry(1, "c", 3, domain.TypeMissionChanged, `{"newMission":7}`), "message.newMission: must be a non-empty string"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := domain.Classify(tc.e)
			var rej *domain.Rejection
			if !errors.As(err, &rej) {
				t.Fatalf("err = %v, want a Rejection", err)
			}
			if !strings.Contains(rej.Reason, tc.reason) {
				t.Fatalf("reason %q does not contain %q", rej.Reason, tc.reason)
			}
		})
	}
}

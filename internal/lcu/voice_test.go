package lcu

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/tk-57/lol-settings/internal/profile"
)

func TestApplyClearsNullVoiceBindingBeforeRestoringCharacterMenu(t *testing.T) {
	c, m, a, _ := fixture(t)
	m.input = profile.Settings{"GameEvents": {"evtPushToTalk": "[c]", "evtPushToTalkTeam": nil, "evtShowCharacterMenu": "[<Unbound>]"}}
	source := &profile.Profile{Input: profile.Settings{"GameEvents": {"evtPushToTalk": nil, "evtPushToTalkTeam": "", "evtShowCharacterMenu": "[c]"}}}
	transport := c.http.Transport
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if r.Method == "PATCH" && r.URL.Path == inputEndpoint {
			// Reproduce the observed client: account voice preferences own PTT,
			// unbound can read back as null, and C cannot also be the menu key.
			party := m.voice["pushToTalkKey"]
			if party == "[<Unbound>]" {
				party = nil
			}
			m.input["GameEvents"]["evtPushToTalk"] = party
			m.input["GameEvents"]["evtPushToTalkTeam"] = nil
			if party == "[c]" && m.input["GameEvents"]["evtShowCharacterMenu"] == "[c]" {
				m.input["GameEvents"]["evtShowCharacterMenu"] = "[<Unbound>]"
			}
		}
		return response, err
	})
	prepared, err := c.Prepare(a, source)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan.Changed != 2 || prepared.Plan.Input["GameEvents"]["evtPushToTalk"] != "[<Unbound>]" || len(prepared.Plan.Skipped) != 0 {
		t.Fatalf("null PTT was not scheduled for clearing: %#v", prepared.Plan)
	}
	if err := c.Apply(prepared); err != nil {
		t.Fatal(err)
	}
	if len(m.patches) < 2 || m.patches[0] != voiceEndpoint || m.patches[1] != inputEndpoint {
		t.Fatalf("incorrect write order: %v", m.patches)
	}
	if err := verifySettings(inputEndpoint, source.Input, m.input); err != nil {
		t.Fatal(err)
	}
	if m.voice["autoJoin"] != true || m.voice["muteOnConnect"] != true || m.voice["pushToTalkKey"] != "[<Unbound>]" {
		t.Fatalf("unexpected voice preferences: %#v", m.voice)
	}
	if plan := profile.MakePlan(source, &profile.Profile{Input: m.input}); plan.Changed != 0 {
		t.Fatal("null representation causes repeated changes")
	}
}

func TestVoiceChangeAfterConfirmationStopsAllWrites(t *testing.T) {
	c, m, a, source := fixture(t)
	m.input["GameEvents"] = map[string]any{"evtPushToTalk": "[c]"}
	source.Input["GameEvents"] = map[string]any{"evtPushToTalk": nil}
	prepared, err := c.Prepare(a, source)
	if err != nil {
		t.Fatal(err)
	}
	m.voice["pushToTalkKey"] = "[b]"
	if err := c.Apply(prepared); err == nil || !strings.Contains(err.Error(), "通話キーが変わりました") || len(m.patches) != 0 {
		t.Fatalf("stale confirmation was used: %v / %v", err, m.patches)
	}
}

func TestRejectedVoiceWriteStopsInputChanges(t *testing.T) {
	c, m, a, _ := fixture(t)
	m.input = profile.Settings{"GameEvents": {"evtPushToTalk": "[c]"}}
	source := &profile.Profile{Input: profile.Settings{"GameEvents": {"evtPushToTalk": nil}}}
	prepared, err := c.Prepare(a, source)
	if err != nil {
		t.Fatal(err)
	}
	m.ignorePatch = true
	if err := c.Apply(prepared); err == nil || len(m.patches) != 1 || m.patches[0] != voiceEndpoint {
		t.Fatalf("continued after rejected voice update: %v / %v", err, m.patches)
	}
}

func TestVoiceChangesHandlePartyAndActiveTeamScheme(t *testing.T) {
	input := profile.Settings{
		"GameEvents": {"evtPushToTalk": nil, "evtPushToTalkTeam": "", "evtShowCharacterMenu": "[c]"},
		"WASD":       {"evtPushToTalkTeam": "[b]"},
	}
	point := voiceChanges(input, nil)
	wasd := voiceChanges(input, profile.Settings{"HUD": {"Movement Mode": json.Number("1")}})
	if len(point) != 2 || point["pushToTalkKey"] != "[<Unbound>]" || point["pushToTalkTeamKey"] != "[<Unbound>]" || len(wasd) != 2 || wasd["pushToTalkKey"] != "[<Unbound>]" || wasd["pushToTalkTeamKey"] != "[b]" {
		t.Fatalf("incorrect voice mapping: %#v / %#v", point, wasd)
	}
}

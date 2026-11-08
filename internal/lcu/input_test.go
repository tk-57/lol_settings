package lcu

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/tk-57/lol-settings/internal/profile"
)

func TestApplyReleasesOldBindingsBeforeAssigningKeys(t *testing.T) {
	c, m, a, _ := fixture(t)
	// Synthetic bindings exercise releasing a key before assigning it elsewhere.
	m.input = profile.Settings{"GameEvents": {
		"evtPlayerAttackMove":  "[<Unbound>],[<Unbound>]",
		"evtRadialEmoteOpen":   "[v]",
		"evtShowCharacterMenu": "[<Unbound>]",
	}}
	source := &profile.Profile{Input: profile.Settings{"GameEvents": {
		"evtPlayerAttackMove":  "[v],[<Unbound>]",
		"evtRadialEmoteOpen":   "",
		"evtShowCharacterMenu": "[c]",
	}}}
	transport := c.http.Transport
	var inputWrites []profile.Settings
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "PATCH" || r.URL.Path != inputEndpoint {
			return transport.RoundTrip(r)
		}
		body, _ := io.ReadAll(r.Body)
		settings, err := profile.DecodeSettings(body)
		if err != nil {
			t.Fatal(err)
		}
		inputWrites = append(inputWrites, profile.WithChanges(nil, settings))
		// Simulate a client that ignores empty strings and refuses keys still
		// assigned to another action. The old single PATCH fails this scenario.
		var keys []string
		for key := range settings["GameEvents"] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := settings["GameEvents"][key].(string)
			if value == "" {
				delete(settings["GameEvents"], key)
				continue
			}
			for other, old := range m.input["GameEvents"] {
				if other != key && old == "[v]" && strings.Contains(value, "[v]") {
					settings["GameEvents"][key] = "[<Unbound>],[<Unbound>]"
				}
			}
		}
		body, _ = json.Marshal(settings)
		r.Body = io.NopCloser(bytes.NewReader(body))
		return transport.RoundTrip(r)
	})
	prepared, err := c.Prepare(a, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Apply(prepared); err != nil {
		t.Fatal(err)
	}
	if len(inputWrites) != 2 || profile.Count(inputWrites[0]) != 1 || inputWrites[0]["GameEvents"]["evtRadialEmoteOpen"] != "[<Unbound>]" {
		t.Fatalf("did not release the old binding first: %#v", inputWrites)
	}
	if err := verifySettings(inputEndpoint, source.Input, m.input); err != nil {
		t.Fatal(err)
	}
	if source.Input["GameEvents"]["evtRadialEmoteOpen"] != "" {
		t.Fatal("source profile was changed")
	}
	if plan := profile.MakePlan(source, &profile.Profile{Input: m.input}); plan.Changed != 0 {
		t.Fatal("reapplying the same file would change unbound markers again")
	}
}

func TestApplyDetectsSideEffectsOnUnchangedBindings(t *testing.T) {
	c, m, a, source := fixture(t)
	source.Game = nil
	m.input["GameEvents"] = map[string]any{"evtShowCharacterMenu": "[c]"}
	transport := c.http.Transport
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if r.Method == "PATCH" && r.URL.Path == inputEndpoint {
			m.input["GameEvents"]["evtShowCharacterMenu"] = "[<Unbound>]"
		}
		return response, err
	})
	prepared, err := c.Prepare(a, source)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Apply(prepared)
	if err == nil || !strings.Contains(err.Error(), "GameEvents.evtShowCharacterMenu") || !strings.Contains(err.Error(), `指定 "[c]"`) || !strings.Contains(err.Error(), `実際 "[<Unbound>]"`) {
		t.Fatalf("unintended unbind was not reported: %v", err)
	}
}

func TestAccountSwitchAfterUnbindStopsAssignment(t *testing.T) {
	c, m, a, _ := fixture(t)
	m.input = profile.Settings{"GameEvents": {"evtRadialEmoteOpen": "[v]"}}
	source := &profile.Profile{Input: profile.Settings{"GameEvents": {"evtRadialEmoteOpen": "[b]"}}}
	prepared, err := c.Prepare(a, source)
	if err != nil {
		t.Fatal(err)
	}
	transport := c.http.Transport
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if r.Method == "PATCH" {
			m.account = "another-account"
		}
		return response, err
	})
	if err := c.Apply(prepared); err == nil || !strings.Contains(err.Error(), "一部の設定が変更済み") {
		t.Fatalf("session change after unbind was not reported: %v", err)
	}
	if len(m.patches) != 1 || m.patches[0] != inputEndpoint || m.input["GameEvents"]["evtRadialEmoteOpen"] != "[<Unbound>]" {
		t.Fatal("assigned a key after the account changed")
	}
}

func TestVerifySettingsDistinguishesMissingAndActiveBindings(t *testing.T) {
	for _, actual := range []profile.Settings{
		{},
		{"GameEvents": {"binding": "[v]"}},
	} {
		if err := verifySettings(inputEndpoint, profile.Settings{"GameEvents": {"binding": ""}}, actual); err == nil {
			t.Fatalf("accepted an incorrect binding: %#v", actual)
		}
	}
	if err := verifySettings(inputEndpoint, profile.Settings{"GameEvents": {"binding": ""}}, profile.Settings{"GameEvents": {"binding": nil}}); err != nil {
		t.Fatalf("null did not match an unbound key: %v", err)
	}
	if err := verifySettings(gameEndpoint, profile.Settings{"General": {"text": ""}}, profile.Settings{"General": {"text": "[<Unbound>]"}}); err == nil {
		t.Fatal("applied input equivalence to game settings")
	}
}

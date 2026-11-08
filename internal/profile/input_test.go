package profile

import (
	"encoding/json"
	"testing"
)

func TestInputUnboundEquivalence(t *testing.T) {
	for _, tc := range []struct {
		section string
		a, b    any
		equal   bool
	}{
		{"GameEvents", "", "[<Unbound>]", true},
		{"WASD", "", "[<Unbound>],[<Unbound>]", true},
		{"HUDEvents", ",", "", true},
		{"ShopEvents", "[<Unbound>],", "", true},
		{"GameEvents", nil, "", true},
		{"GameEvents", nil, "[<Unbound>]", true},
		{"Quickbinds", nil, "", false},
		{"GameEvents", "[v]", "[<Unbound>]", false},
		{"GameEvents", "[v],[<Unbound>]", "[<Unbound>],[v]", false},
		{"GameEvents", "[Ctrl][v]", "[v]", false},
		{"Quickbinds", "", "[<Unbound>]", false},
		{"Quickbinds", json.Number("1"), "1", false},
	} {
		if got := SameInputValue(tc.section, tc.a, tc.b); got != tc.equal {
			t.Fatalf("%s: %v / %v: got %v", tc.section, tc.a, tc.b, got)
		}
	}
}

func TestPlanExplicitlyUnbindsEmptyKeys(t *testing.T) {
	source := &Profile{
		Game: Settings{"General": {"text": ""}},
		Input: Settings{
			"GameEvents": {"evtRadialEmoteOpen": "", "unchanged": "", "unknown": nil},
			"Quickbinds": {"enabled": json.Number("0")},
		},
	}
	current := &Profile{
		Game: Settings{"General": {"text": "old"}},
		Input: Settings{
			"GameEvents": {"evtRadialEmoteOpen": "[v]", "unchanged": "[<Unbound>],[<Unbound>]", "unknown": nil},
			"Quickbinds": {"enabled": json.Number("1")},
		},
	}
	plan := MakePlan(source, current)
	if plan.Changed != 3 || len(plan.Skipped) != 0 || plan.Input["GameEvents"]["evtRadialEmoteOpen"] != unboundKey {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.Game["General"]["text"] != "" || source.Input["GameEvents"]["evtRadialEmoteOpen"] != "" {
		t.Fatal("changed a game setting or mutated the source profile")
	}
	unbinds := InputUnbinds(plan.Input, current.Input)
	if Count(unbinds) != 1 || unbinds["GameEvents"]["evtRadialEmoteOpen"] != unboundKey {
		t.Fatalf("unexpected unbinds: %#v", unbinds)
	}
}

func TestInputUnbindsPreserveUnchangedAndNonBindingSettings(t *testing.T) {
	before := Settings{
		"GameEvents": {"changed": "[a],[b]", "untouched": "[c]", "empty": "", "unknown": nil},
		"Quickbinds": {"changed": json.Number("1")},
	}
	changes := Settings{
		"GameEvents": {"changed": "[v]", "empty": "[x]", "unknown": nil},
		"Quickbinds": {"changed": json.Number("0")},
	}
	unbinds := InputUnbinds(changes, before)
	if Count(unbinds) != 1 || unbinds["GameEvents"]["changed"] != "[<Unbound>],[<Unbound>]" {
		t.Fatalf("unexpected unbinds: %#v", unbinds)
	}
	wanted := WithChanges(before, changes)
	if wanted["GameEvents"]["untouched"] != "[c]" || wanted["GameEvents"]["changed"] != "[v]" {
		t.Fatal("expected snapshot lost bindings")
	}
	wanted["GameEvents"]["changed"] = "[q]"
	if before["GameEvents"]["changed"] != "[a],[b]" || changes["GameEvents"]["changed"] != "[v]" {
		t.Fatal("expected snapshot aliases inputs")
	}
}

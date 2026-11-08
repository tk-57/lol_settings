package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactNumericComparison(t *testing.T) {
	for _, tc := range []struct {
		a, b  string
		equal bool
	}{
		{"10", "10.0", true},
		{"1e2", "100", true},
		{"-0", "0.0", true},
		{"9007199254740992", "9007199254740993", false},
		{"0.10000000000000001", "0.1", false},
		{"1e-999999999", "0", false},
	} {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			if got := SameValue(json.Number(tc.a), json.Number(tc.b)); got != tc.equal {
				t.Fatalf("SameValue(%s, %s) = %v", tc.a, tc.b, got)
			}
		})
	}
}

func TestRejectInvalidSettingNamesAndNumbers(t *testing.T) {
	for _, data := range []string{
		`{"gameSettings":{"General":{"x":1e309}}}`,
		`{"gameSettings":{"General":{"x":1e-999999999}}}`,
		`{"gameSettings":{"General":{"x\u0000":1}}}`,
		`{"gameSettings":{"General\n":{"x":1}}}`,
		`{"gameSettings":{"General":{" ":1}}}`,
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestSaveReplacesExistingFileAndCleansTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("previous content"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Profile{FormatVersion: 1, Game: Settings{"General": {"volume": json.Number("10")}}}
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	actual, err := Load(path)
	if err != nil || !Matches(p.Game, actual.Game) {
		t.Fatalf("saved profile did not roundtrip: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("temporary file left behind: %v, %v", entries, err)
	}
}

func TestInvalidSavePreservesPreviousFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, p := range []*Profile{
		{},
		{Game: Settings{"General": {"x": []int{1}}}},
		{Game: Settings{"General": {"x": strings.Repeat("x", MaxFileSize)}}},
	} {
		if err := os.WriteFile(path, []byte("previous content"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := Save(path, p); err == nil {
			t.Fatal("invalid profile saved")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "previous content" {
			t.Fatalf("existing file changed: %q, %v", data, err)
		}
	}
}

func TestNullBindingRoundtrip(t *testing.T) {
	p, err := Parse([]byte(`{"inputSettings":{"GameEvents":{"optionalBinding":null,"evtCastSpell1":"[q]"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	value, exists := loaded.Input["GameEvents"]["optionalBinding"]
	if !exists || value != nil || loaded.Input["GameEvents"]["evtCastSpell1"] != "[q]" {
		t.Fatalf("bindings were not preserved: %#v", loaded.Input)
	}
	if !Matches(p.Input, loaded.Input) || !Matches(loaded.Input, p.Input) {
		t.Fatal("null binding does not match after roundtrip")
	}
	if plan := MakePlan(p, loaded); plan.Changed != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("unchanged null binding affected plan: %#v", plan)
	}
}

func TestNullBindingsDoNotOverwriteTypedSettings(t *testing.T) {
	for _, values := range [][2]any{{nil, "[v]"}, {"[v]", nil}} {
		source := &Profile{Game: Settings{"General": {"optionalSetting": values[0]}}}
		current := &Profile{Game: Settings{"General": {"optionalSetting": values[1]}}}
		plan := MakePlan(source, current)
		if plan.Changed != 0 || len(plan.Skipped) != 1 {
			t.Fatalf("null/type transition was not skipped: %#v", plan)
		}
		if Matches(source.Game, current.Game) {
			t.Fatal("null matched a typed value")
		}
	}
	if Matches(Settings{"GameEvents": {"optionalBinding": nil}}, Settings{}) {
		t.Fatal("null matched a missing key")
	}
}

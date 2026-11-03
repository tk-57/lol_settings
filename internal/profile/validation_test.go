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

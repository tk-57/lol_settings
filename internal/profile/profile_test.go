package profile

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyProfileRoundtrip(t *testing.T) {
	// A synthetic profile keeps this test independent of personal settings.
	data := []byte(`{"gameSettings":"{\"General\":{\"volume\":10}}","inputSettings":"{\"GameEvents\":{\"evtCastSpell1\":\"[q]\"}}"}`)
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Input["GameEvents"]["evtCastSpell1"] != "[q]" {
		t.Fatal("legacy key binding was lost")
	}
	if len(p.Game) == 0 || len(p.Input) == 0 {
		t.Fatal("empty legacy profile")
	}
	out := filepath.Join(t.TempDir(), "roundtrip.json")
	if err = Save(out, p); err != nil {
		t.Fatal(err)
	}
	q, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(p.Game, q.Game) || !Matches(p.Input, q.Input) {
		t.Fatal("roundtrip lost values")
	}
}

func TestMalformedProfiles(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"gameSettings":null}`, `{"gameSettings":{}}`, `{"gameSettings":{"General":null}}`, `{"gameSettings":{"General":{"x":[]}}}`, `{"gameSettings":"broken"}`, `{"formatVersion":2,"gameSettings":{"General":{"x":1}}}`, `{"gameSettings":{"General":{"x":1}}} {}`, strings.Repeat(" ", MaxFileSize+1)} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Fatalf("accepted malformed profile %.80s", data)
		}
	}
}

func TestCompatibilityForBothCategories(t *testing.T) {
	source, _ := Parse([]byte(`{"gameSettings":{"General":{"a":10.0,"b":false,"gone":1,"wrong":"yes"}},"inputSettings":{"Keys":{"q":"[w]"}}}`))
	current, _ := Parse([]byte(`{"gameSettings":{"General":{"a":10,"b":true,"wrong":true}},"inputSettings":{"Keys":{"q":"[q]"}}}`))
	p := MakePlan(source, current)
	if p.Changed != 2 || len(p.Skipped) != 2 || p.Input["Keys"]["q"] != "[w]" || p.Game["General"]["b"] != false {
		t.Fatalf("wrong plan: %#v", p)
	}
	if _, ok := p.Game["General"]["a"]; ok {
		t.Fatal("numeric representation should not count as a change")
	}
}

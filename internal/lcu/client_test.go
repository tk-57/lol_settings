package lcu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk-57/lol-settings/internal/profile"
)

func TestLockfileValidation(t *testing.T) {
	c, err := parseLockfile([]byte("LeagueClient:123:50000:secret:https\r\n"))
	if err != nil || c.Port != 50000 {
		t.Fatal(c.Port, err)
	}
	for _, s := range []string{"RiotClient:123:42:secret:https", "LeagueClient:123:70000:secret:https", "LeagueClient:0:42:secret:https", "LeagueClient:123:42::https", "LeagueClient:123:42:secret:http", "broken"} {
		if _, err := parseLockfile([]byte(s)); err == nil {
			t.Fatal("accepted invalid lockfile")
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type mockLCU struct {
	t                      *testing.T
	game, input            profile.Settings
	account, phase         string
	patches                []string
	failInput, ignorePatch bool
	voice                  map[string]any
}

func fixture(t *testing.T) (*Client, *mockLCU, Account, *profile.Profile) {
	m := &mockLCU{t: t, game: profile.Settings{"General": {"volume": json.Number("10")}}, input: profile.Settings{"Keys": {"q": "[q]"}}, account: "account-a", phase: "None"}
	m.voice = map[string]any{"pushToTalkKey": "[c]", "pushToTalkTeamKey": "[<Unbound>]", "autoJoin": true, "muteOnConnect": true}
	c := &Client{credentials: credentials{123, 12345, "test-secret"}, http: &http.Client{Transport: roundTripFunc(m.respond)}}
	source := &profile.Profile{Game: profile.Settings{"General": {"volume": json.Number("20")}}, Input: profile.Settings{"Keys": {"q": "[w]"}}}
	return c, m, Account{PUUID: "account-a", GameName: "Player", TagLine: "JP1"}, source
}

func (m *mockLCU) respond(r *http.Request) (*http.Response, error) {
	m.t.Helper()
	if r.URL.Hostname() != "127.0.0.1" || r.URL.Scheme != "https" {
		m.t.Fatal("non-loopback request")
	}
	u, p, ok := r.BasicAuth()
	if !ok || u != "riot" || p != "test-secret" {
		m.t.Fatal("missing authentication")
	}
	status := 200
	var payload any
	path := r.URL.Path
	if r.Method == "PATCH" {
		m.patches = append(m.patches, path)
		if path == voiceEndpoint {
			var preferences voicePreferences
			if err := json.NewDecoder(r.Body).Decode(&preferences); err != nil || preferences.SchemaVersion != 1 {
				m.t.Fatal("invalid voice preference update", err)
			}
			for field, value := range preferences.Data {
				if field != "pushToTalkKey" && field != "pushToTalkTeamKey" {
					m.t.Fatal("unexpected voice setting update", field)
				}
				if !m.ignorePatch {
					m.voice[field] = value
				}
			}
			status = 204
		} else if path == inputEndpoint && m.failInput {
			status = 500
			payload = map[string]string{"error": "simulated"}
		} else {
			b, _ := io.ReadAll(r.Body)
			settings, err := profile.DecodeSettings(b)
			if err != nil {
				m.t.Fatal(err)
			}
			if !m.ignorePatch {
				dst := m.game
				if path == inputEndpoint {
					dst = m.input
				}
				for section, entries := range settings {
					for key, v := range entries {
						dst[section][key] = v
					}
				}
			}
			status = 204
		}
	} else {
		switch path {
		case "/lol-summoner/v1/current-summoner":
			payload = map[string]string{"puuid": m.account, "gameName": "Player", "tagLine": "JP1"}
		case "/lol-gameflow/v1/gameflow-phase":
			payload = m.phase
		case gameEndpoint:
			payload = m.game
		case inputEndpoint:
			payload = m.input
		case voiceEndpoint:
			payload = voicePreferences{SchemaVersion: 1, Data: m.voice}
		default:
			m.t.Fatal("unexpected path", path)
		}
	}
	b, _ := json.Marshal(payload)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}}, nil
}

func TestApplyBothCategoriesWithoutBackup(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	c, m, a, p := fixture(t)
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Apply(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.patches) != 2 || m.input["Keys"]["q"] != "[w]" || !profile.SameValue(m.game["General"]["volume"], json.Number("20")) {
		t.Fatal("apply did not finish")
	}
	files, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("app created automatic backup data")
	}
}

func TestAccountSwitchBlocksWrites(t *testing.T) {
	c, m, a, p := fixture(t)
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	m.account = "account-b"
	if err = c.Apply(prepared); err == nil || len(m.patches) != 0 {
		t.Fatal("write after account switch")
	}
}

func TestGameInProgressBlocksWrites(t *testing.T) {
	for _, phase := range []string{"Matchmaking", "ReadyCheck", "ChampSelect", "InProgress", "Reconnect", "Unknown"} {
		t.Run(phase, func(t *testing.T) {
			c, m, a, p := fixture(t)
			m.phase = phase
			if _, err := c.Prepare(a, p); err == nil {
				t.Fatal("unsafe phase accepted")
			}
			if len(m.patches) != 0 {
				t.Fatal("unexpected write")
			}
		})
	}
}

func TestSettingsChangedSincePreview(t *testing.T) {
	c, m, a, p := fixture(t)
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	m.input["Keys"]["q"] = "[e]"
	if err = c.Apply(prepared); err == nil || len(m.patches) != 0 {
		t.Fatal("applied stale preview")
	}
}

func TestPartialFailureIsReported(t *testing.T) {
	c, m, a, p := fixture(t)
	m.failInput = true
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Apply(prepared)
	if err == nil || !strings.Contains(err.Error(), "一部の設定が変更済み") {
		t.Fatal("partial failure not reported")
	}
	if m.input["Keys"]["q"] != "[q]" {
		t.Fatal("unexpected input mutation")
	}
}

func TestSilentRejectionIsDetected(t *testing.T) {
	c, m, a, p := fixture(t)
	m.ignorePatch = true
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Apply(prepared)
	if err == nil || len(m.patches) != 1 {
		t.Fatal("silent rejection was reported as success")
	}
}

func TestHTTPFailuresAreSafeAndHelpful(t *testing.T) {
	for _, status := range []int{301, 400, 401, 403, 404, 405, 422, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, _, _, _ := fixture(t)
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("test-secret")), Header: http.Header{}}, nil
			})
			_, err := c.request("GET", gameEndpoint, nil)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "test-secret") {
				t.Fatal("unsafe error", err)
			}
		})
	}
}

func TestPartialLegacyProfile(t *testing.T) {
	c, m, a, p := fixture(t)
	p.Game = nil
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Apply(prepared); err != nil {
		t.Fatal(err)
	}
	if len(m.patches) != 1 || m.patches[0] != inputEndpoint {
		t.Fatal("wrote a category absent from the file")
	}
}

func TestLockfileRotationBlocksOperation(t *testing.T) {
	c, m, a, p := fixture(t)
	lock := filepath.Join(t.TempDir(), "lockfile")
	c.lockpath = lock
	if err := os.WriteFile(lock, []byte("LeagueClient:123:12345:test-secret:https"), 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("LeagueClient:123:12345:new-secret:https"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.Apply(prepared); err == nil || len(m.patches) != 0 {
		t.Fatal("rotated credentials accepted")
	}
}

func TestSnapshotSaveAndApplyPreservePushToTalk(t *testing.T) {
	c, m, a, _ := fixture(t)
	m.input["GameEvents"] = map[string]any{"evtPushToTalk": nil, "evtPushToTalkTeam": nil}
	p, err := c.Snapshot(a)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := profile.Save(path, p); err != nil {
		t.Fatal(err)
	}
	p, err = profile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"evtPushToTalk", "evtPushToTalkTeam"} {
		if value, exists := p.Input["GameEvents"][key]; !exists || value != nil {
			t.Fatalf("voice binding %s was lost", key)
		}
	}
	m.input["Keys"]["q"] = "[w]"
	prepared, err := c.Prepare(a, p)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan.Changed != 1 || len(prepared.Plan.Skipped) != 0 {
		t.Fatalf("unexpected plan: %#v", prepared.Plan)
	}
	if err := c.Apply(prepared); err != nil {
		t.Fatal(err)
	}
	if len(m.patches) != 1 || m.patches[0] != inputEndpoint || m.input["Keys"]["q"] != "[q]" {
		t.Fatal("binding was not restored")
	}
}

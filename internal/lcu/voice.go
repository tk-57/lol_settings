package lcu

import (
	"encoding/json"
	"fmt"

	"github.com/tk-57/lol-settings/internal/jsonutil"
	"github.com/tk-57/lol-settings/internal/profile"
)

// The Voice settings UI writes this account preference, not input-settings alone.
const voiceEndpoint = "/lol-settings/v2/account/LCUPreferences/lol-premade-voice"

type voicePreferences struct {
	SchemaVersion int            `json:"schemaVersion"`
	Data          map[string]any `json:"data"`
}

func voiceChanges(input, game profile.Settings) map[string]any {
	teamSection := "GameEvents"
	if profile.SameValue(game["HUD"]["Movement Mode"], json.Number("1")) {
		teamSection = "WASD"
	}
	out := map[string]any{}
	for _, binding := range []struct{ section, key, field string }{
		{"GameEvents", "evtPushToTalk", "pushToTalkKey"},
		{teamSection, "evtPushToTalkTeam", "pushToTalkTeamKey"},
	} {
		if value, exists := input[binding.section][binding.key]; exists {
			if normalized, ok := profile.InputValue(binding.section, value).(string); ok {
				out[binding.field] = normalized
			}
		}
	}
	return out
}

func (c *Client) voicePreferences() (*voicePreferences, error) {
	b, err := c.request("GET", voiceEndpoint, nil)
	if err != nil {
		return nil, err
	}
	var settings voicePreferences
	if err := jsonutil.Decode(b, &settings); err != nil || settings.Data == nil || settings.SchemaVersion != 1 {
		return nil, fmt.Errorf("通話キーのアカウント設定を読み取れません")
	}
	return &settings, nil
}

func sameVoiceFields(before, current, fields map[string]any) bool {
	for field := range fields {
		a, hasA := before[field]
		b, hasB := current[field]
		left, errA := json.Marshal(a)
		right, errB := json.Marshal(b)
		if hasA != hasB || errA != nil || errB != nil || string(left) != string(right) {
			return false
		}
	}
	return true
}

func (c *Client) updateVoiceBindings(a Account, changes map[string]any) error {
	if err := c.guard(a); err != nil {
		return err
	}
	// Send only PTT fields, preserving microphone, mute and auto-join preferences.
	if _, err := c.request("PATCH", voiceEndpoint, voicePreferences{SchemaVersion: 1, Data: changes}); err != nil {
		return err
	}
	if err := c.guard(a); err != nil {
		return err
	}
	actual, err := c.voicePreferences()
	if err != nil {
		return err
	}
	for field, wanted := range changes {
		got, exists := actual.Data[field]
		if got != nil {
			if _, ok := got.(string); !ok {
				return fmt.Errorf("通話キー %s の応答形式が不正です", field)
			}
		}
		if !exists || !profile.SameInputValue("GameEvents", wanted, got) {
			return fmt.Errorf("通話キー %s が反映されませんでした: 指定 %s → 実際 %s", field, settingText(wanted), settingText(got))
		}
	}
	return nil
}

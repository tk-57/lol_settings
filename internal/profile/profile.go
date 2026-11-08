// Package profile handles settings files and computes compatible changes.
package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tk-57/lol-settings/internal/jsonutil"
)

// MaxFileSize bounds both settings files and LCU responses to 4 MiB.
const MaxFileSize = 4 << 20

// Settings maps section names to scalar setting values, including null.
type Settings map[string]map[string]any

// Profile is the on-disk JSON format. It does not contain account credentials.
type Profile struct {
	FormatVersion int      `json:"formatVersion,omitempty"`
	SavedAt       string   `json:"savedAt,omitempty"`
	Game          Settings `json:"gameSettings,omitempty"`
	Input         Settings `json:"inputSettings,omitempty"`
}

// DecodeSettings validates section contents from a file or LCU response.
func DecodeSettings(raw json.RawMessage) (Settings, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var nested string
		if err := json.Unmarshal(raw, &nested); err != nil {
			return nil, err
		}
		raw = []byte(nested)
	}
	var s Settings
	if err := jsonutil.Decode(raw, &s); err != nil {
		return nil, err
	}
	if len(s) == 0 {
		return nil, fmt.Errorf("設定が空です")
	}
	for section, entries := range s {
		if strings.TrimSpace(section) == "" || strings.ContainsAny(section, "\x00\r\n") || len(entries) == 0 {
			return nil, fmt.Errorf("空の設定セクションがあります")
		}
		for key, v := range entries {
			if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\x00\r\n") {
				return nil, fmt.Errorf("空の設定名があります")
			}
			switch v.(type) {
			case nil, bool, string:
			case json.Number:
				if !validNumber(v.(json.Number)) {
					return nil, fmt.Errorf("%s.%s の数値が範囲外です", section, key)
				}
			default:
				return nil, fmt.Errorf("%s.%s の値が不正です", section, key)
			}
		}
	}
	return s, nil
}

// Parse validates a settings document without reading from disk.
func Parse(data []byte) (*Profile, error) {
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("設定ファイルは 4 MB 以下にしてください")
	}
	var raw struct {
		FormatVersion int             `json:"formatVersion"`
		SavedAt       string          `json:"savedAt"`
		Game          json.RawMessage `json:"gameSettings"`
		Input         json.RawMessage `json:"inputSettings"`
	}
	if err := jsonutil.Decode(data, &raw); err != nil {
		return nil, fmt.Errorf("設定ファイルを読めません: %w", err)
	}
	if raw.FormatVersion < 0 || raw.FormatVersion > 1 {
		return nil, fmt.Errorf("未対応の設定ファイル形式です: %d", raw.FormatVersion)
	}
	p := &Profile{FormatVersion: 1, SavedAt: raw.SavedAt}
	var err error
	if p.Game, err = DecodeSettings(raw.Game); err != nil {
		return nil, fmt.Errorf("ゲーム設定: %w", err)
	}
	if p.Input, err = DecodeSettings(raw.Input); err != nil {
		return nil, fmt.Errorf("キー設定: %w", err)
	}
	if p.Game == nil && p.Input == nil {
		return nil, fmt.Errorf("gameSettings / inputSettings がありません")
	}
	return p, nil
}

// Load reads a bounded JSON file.
func Load(path string) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Save writes through a temporary file in the destination directory.
// A validation or write failure leaves the previous destination untouched.
func Save(path string, p *Profile) error {
	if p == nil {
		return fmt.Errorf("保存する設定がありません")
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := Parse(b); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lolsettings-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Count returns the number of leaf settings.
func Count(settings Settings) int {
	count := 0
	for _, entries := range settings {
		count += len(entries)
	}
	return count
}

// Limit precision and exponent size before using exact arithmetic on file input.
func validNumber(n json.Number) bool {
	value := string(n)
	if len(value) > 128 {
		return false
	}
	if _, err := n.Float64(); err != nil {
		return false
	}
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(value[index+1:])
		if err != nil || exponent < -308 || exponent > 308 {
			return false
		}
	}
	return true
}

// Package lcu communicates with the local League client.
package lcu

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tk-57/lol-settings/internal/jsonutil"
	"github.com/tk-57/lol-settings/internal/profile"
)

const gameEndpoint = "/lol-game-settings/v1/game-settings"
const inputEndpoint = "/lol-game-settings/v1/input-settings"

type credentials struct {
	PID, Port int
	Token     string
}

func parseLockfile(b []byte) (credentials, error) {
	parts := strings.Split(strings.TrimSpace(string(b)), ":")
	if len(parts) != 5 || parts[0] != "LeagueClient" || parts[4] != "https" || parts[3] == "" {
		return credentials{}, fmt.Errorf("League of Legends の lockfile ではありません")
	}
	pid, e1 := strconv.Atoi(parts[1])
	port, e2 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || pid < 1 || port < 1 || port > 65535 {
		return credentials{}, fmt.Errorf("lockfile の PID / ポートが不正です")
	}
	return credentials{PID: pid, Port: port, Token: parts[3]}, nil
}

// Client is bound to one local client process and its lockfile credentials.
type Client struct {
	credentials credentials
	lockpath    string
	http        *http.Client
}

func newClient(path string) (*Client, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := parseLockfile(b)
	if err != nil {
		return nil, err
	}
	// Only loopback is used, without proxy or redirects. LCU uses a self-signed certificate.
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}
	return &Client{credentials: c, lockpath: path, http: &http.Client{Transport: transport, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Close releases idle HTTP connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) request(method, path string, body any) ([]byte, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, fmt.Sprintf("https://127.0.0.1:%d%s", c.credentials.Port, path), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("riot", c.credentials.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LCU に接続できません。LoL のログイン状態とアクセス権を確認し、再接続してください (%s)", path)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		hint := "クライアントを再起動して再接続してください"
		switch res.StatusCode {
		case 401, 403:
			hint = "認証が変わったか、アクセスが拒否されました。再接続してください"
		case 404, 405:
			hint = "この API が利用できません。Client の仕様変更またはログイン未完了の可能性があります"
		case 400, 422:
			hint = "現在のクライアントが設定値を拒否しました"
		}
		return nil, fmt.Errorf("%s %s: HTTP %d — %s", method, path, res.StatusCode, hint)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, profile.MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("LCU 応答の読み取りに失敗しました")
	}
	if len(b) > profile.MaxFileSize {
		return nil, fmt.Errorf("LCU 応答が大きすぎます")
	}
	return b, nil
}

// Account identifies the active session and its display name.
type Account struct {
	PUUID       string      `json:"puuid"`
	SummonerID  json.Number `json:"summonerId"`
	GameName    string      `json:"gameName"`
	TagLine     string      `json:"tagLine"`
	DisplayName string      `json:"displayName"`
}

func (a Account) key() string {
	if a.PUUID != "" {
		return a.PUUID
	}
	return string(a.SummonerID)
}

// Label returns the Riot ID when available.
func (a Account) Label() string {
	if a.GameName != "" {
		if a.TagLine != "" {
			return a.GameName + "#" + a.TagLine
		}
		return a.GameName
	}
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return "サモナー ID " + string(a.SummonerID)
}

// Account retrieves the current logged-in account.
func (c *Client) Account() (Account, error) {
	b, err := c.request("GET", "/lol-summoner/v1/current-summoner", nil)
	if err != nil {
		return Account{}, err
	}
	var a Account
	if err = jsonutil.Decode(b, &a); err != nil {
		return a, fmt.Errorf("アカウント情報の形式が不正です")
	}
	if a.key() == "" || a.key() == "0" {
		return a, fmt.Errorf("LoL にログインし、ホーム画面まで進めてください")
	}
	return a, nil
}

func (c *Client) guard(expected Account) error {
	if c.lockpath != "" {
		b, e := os.ReadFile(c.lockpath)
		if e != nil {
			return fmt.Errorf("クライアントが終了しました。再接続してください")
		}
		creds, e := parseLockfile(b)
		if e != nil || creds != c.credentials {
			return fmt.Errorf("接続情報が変わりました。再接続してください")
		}
	}
	a, err := c.Account()
	if err != nil {
		return err
	}
	if a.key() != expected.key() {
		return fmt.Errorf("アカウントが切り替わりました。再接続して適用先を確認してください")
	}
	b, err := c.request("GET", "/lol-gameflow/v1/gameflow-phase", nil)
	if err != nil {
		return err
	}
	var phase string
	if err = json.Unmarshal(b, &phase); err != nil {
		return fmt.Errorf("ゲームの状態を確認できません")
	}
	if phase != "None" && phase != "Lobby" {
		return fmt.Errorf("現在は設定を操作できません (%s)。キュー・チャンピオン選択・試合を終了してください", phase)
	}
	return nil
}

// Snapshot reads both settings categories between session checks.
func (c *Client) Snapshot(a Account) (*profile.Profile, error) {
	if err := c.guard(a); err != nil {
		return nil, err
	}
	p := &profile.Profile{FormatVersion: 1, SavedAt: time.Now().UTC().Format(time.RFC3339)}
	b, err := c.request("GET", gameEndpoint, nil)
	if err != nil {
		return nil, err
	}
	if p.Game, err = profile.DecodeSettings(b); err != nil {
		return nil, fmt.Errorf("ゲーム設定の応答形式が不正です: %w", err)
	}
	b, err = c.request("GET", inputEndpoint, nil)
	if err != nil {
		return nil, err
	}
	if p.Input, err = profile.DecodeSettings(b); err != nil {
		return nil, fmt.Errorf("キー設定の応答形式が不正です: %w", err)
	}
	if err = c.guard(a); err != nil {
		return nil, err
	}
	return p, nil
}

// Prepared captures the account and settings shown by the confirmation step.
type Prepared struct {
	Before      *profile.Profile
	Plan        *profile.Plan
	Account     Account
	voiceBefore *voicePreferences
}

// Prepare computes changes without writing to the client.
func (c *Client) Prepare(a Account, p *profile.Profile) (*Prepared, error) {
	before, err := c.Snapshot(a)
	if err != nil {
		return nil, err
	}
	prepared := &Prepared{Before: before, Plan: profile.MakePlan(p, before), Account: a}
	game := profile.WithChanges(before.Game, prepared.Plan.Game)
	if len(voiceChanges(prepared.Plan.Input, game)) > 0 {
		prepared.voiceBefore, err = c.voicePreferences()
		if err != nil {
			return nil, err
		}
		if err := c.guard(a); err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

// Apply rejects stale confirmations and verifies each write by reading it back.
// LCU has no cross-endpoint transaction; failures can leave partial changes.
func (c *Client) Apply(p *Prepared) (err error) {
	wrote := false
	defer func() {
		if err != nil && wrote {
			err = fmt.Errorf("%w\n\n一部の設定が変更済みの可能性があります。", err)
		}
	}()
	if p.Plan.Changed == 0 {
		return fmt.Errorf("適用できる変更はありません")
	}
	current, err := c.Snapshot(p.Account)
	if err != nil {
		return err
	}
	if !profile.Matches(p.Before.Game, current.Game) || !profile.Matches(current.Game, p.Before.Game) || !profile.Matches(p.Before.Input, current.Input) || !profile.Matches(current.Input, p.Before.Input) {
		return fmt.Errorf("確認後に設定が変わりました。もう一度適用内容を確認してください")
	}
	game := profile.WithChanges(current.Game, p.Plan.Game)
	voice := voiceChanges(p.Plan.Input, game)
	if len(voice) > 0 {
		if p.voiceBefore == nil {
			return fmt.Errorf("通話キーの適用内容をもう一度確認してください")
		}
		actual, e := c.voicePreferences()
		if e != nil {
			return e
		}
		if !sameVoiceFields(p.voiceBefore.Data, actual.Data, voice) {
			return fmt.Errorf("確認後に通話キーが変わりました。もう一度適用内容を確認してください")
		}
	}
	// Clear changed bindings before assigning new keys. Verify the full input
	// snapshot afterwards, since assigning a key can affect another binding.
	unbinds := profile.InputUnbinds(p.Plan.Input, current.Input)
	for _, part := range []struct {
		path string
		data profile.Settings
		want profile.Settings
	}{
		{gameEndpoint, p.Plan.Game, p.Plan.Game},
		{inputEndpoint, unbinds, unbinds},
		{inputEndpoint, p.Plan.Input, profile.WithChanges(current.Input, p.Plan.Input)},
	} {
		if len(part.data) == 0 {
			continue
		}
		if err = c.guard(p.Account); err != nil {
			return err
		}
		wrote = true
		if part.path == inputEndpoint {
			if updates := voiceChanges(part.data, game); len(updates) > 0 {
				if err = c.updateVoiceBindings(p.Account, updates); err != nil {
					return err
				}
				if err = c.guard(p.Account); err != nil {
					return err
				}
			}
		}
		if _, err = c.request("PATCH", part.path, part.data); err != nil {
			return err
		}
		if err = c.guard(p.Account); err != nil {
			return err
		}
		b, e := c.request("GET", part.path, nil)
		if e != nil {
			return e
		}
		actual, e := profile.DecodeSettings(b)
		if e != nil {
			return e
		}
		if e := verifySettings(part.path, part.want, actual); e != nil {
			return e
		}
	}
	if err = c.guard(p.Account); err != nil {
		return err
	}
	return nil
}

func verifySettings(path string, wanted, actual profile.Settings) error {
	var differences []string
	for section, entries := range wanted {
		for key, value := range entries {
			got, exists := actual[section][key]
			same := profile.SameValue(value, got)
			if path == inputEndpoint {
				same = profile.SameInputValue(section, value, got)
			}
			if exists && same {
				continue
			}
			expectedText := settingText(value)
			actualText := "項目なし"
			if exists {
				actualText = settingText(got)
			}
			differences = append(differences, fmt.Sprintf("%s.%s: 指定 %s → 実際 %s", section, key, expectedText, actualText))
		}
	}
	if len(differences) == 0 {
		return nil
	}
	sort.Strings(differences)
	count := len(differences)
	if count > 8 {
		differences = append(differences[:8], fmt.Sprintf("ほか %d 項目", count-8))
	}
	return fmt.Errorf("%s: %d 項目が指定どおりに反映されませんでした。\n%s", path, count, strings.Join(differences, "\n"))
}

func settingText(value any) string {
	if s, ok := value.(string); ok {
		if len([]rune(s)) > 80 {
			s = string([]rune(s)[:80]) + "…"
		}
		return strconv.Quote(s)
	}
	b, _ := json.Marshal(value)
	return string(b)
}

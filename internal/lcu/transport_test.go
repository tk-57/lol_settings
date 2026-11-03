package lcu

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk-57/lol-settings/internal/profile"
)

func TestTransportStaysLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lockfile")
	if err := os.WriteFile(path, []byte("LeagueClient:123:12345:test-secret:https"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := newClient(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	transport := c.http.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("environment proxy must not receive local credentials")
	}
	if err := c.http.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatal("redirect must not be followed")
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "127.0.0.1:12345" || r.URL.Scheme != "https" {
			t.Fatalf("unexpected destination: %s", r.URL)
		}
		return nil, errors.New("transport details containing test-secret")
	})
	_, err = c.request(http.MethodGet, gameEndpoint, nil)
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("transport error leaked credentials: %v", err)
	}
}

func TestOversizedResponseRejected(t *testing.T) {
	c, _, _, _ := fixture(t)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", profile.MaxFileSize+1))),
			Header:     http.Header{},
		}, nil
	})
	if _, err := c.request(http.MethodGet, gameEndpoint, nil); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestSnapshotOmitsAccountIdentity(t *testing.T) {
	c, _, account, _ := fixture(t)
	p, err := c.Snapshot(account)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := profile.Save(path, p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{account.PUUID, account.GameName, "test-secret", `"source"`} {
		if strings.Contains(string(data), value) {
			t.Fatalf("saved settings contain account data: %s", value)
		}
	}
}

func TestAccountSwitchDuringApplyStopsRemainingWrites(t *testing.T) {
	c, mock, account, source := fixture(t)
	prepared, err := c.Prepare(account, source)
	if err != nil {
		t.Fatal(err)
	}
	transport := c.http.Transport
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if r.Method == http.MethodPatch {
			mock.account = "account-b"
		}
		return response, err
	})
	err = c.Apply(prepared)
	if err == nil || !strings.Contains(err.Error(), "一部の設定が変更済み") {
		t.Fatalf("session change after a write was not reported: %v", err)
	}
	if len(mock.patches) != 1 || mock.input["Keys"]["q"] != "[q]" {
		t.Fatal("continued writing after account switch")
	}
}

func TestSnapshotRejectsSessionChangeBetweenReads(t *testing.T) {
	c, mock, account, _ := fixture(t)
	transport := c.http.Transport
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if r.URL.Path == gameEndpoint {
			mock.account = "account-b"
		}
		return response, err
	})
	if _, err := c.Snapshot(account); err == nil {
		t.Fatal("snapshot mixed two account sessions")
	}
}

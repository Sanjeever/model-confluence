package oauthlogin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLifecycle(t *testing.T) {
	for _, outcome := range []string{"success", "cancel", "timeout", "save_error"} {
		t.Run(outcome, func(t *testing.T) {
			m := NewManager()
			defer m.CancelOwner("")
			var saves, exchanges atomic.Int32
			started, release := make(chan struct{}), make(chan struct{})
			config := Config{Redirect: "http://127.0.0.1:0/callback", Authorize: func(state, challenge, redirect string) string {
				return "https://example.com/authorize?" + url.Values{"state": {state}, "code_challenge": {challenge}, "redirect_uri": {redirect}}.Encode()
			}}
			config.Exchange = func(ctx context.Context, code, verifier, redirect string) (func() (int64, error), error) {
				exchanges.Add(1)
				if code != "test-code" {
					t.Error("missing code or verifier")
				}
				close(started)
				<-release
				return func() (int64, error) {
					saves.Add(1)
					if outcome == "save_error" {
						return 0, errors.New("save failed")
					}
					return 7, nil
				}, nil
			}
			status, err := m.Start("owner", 1, config)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(status.URL)
			m.mu.Lock()
			expected := sha256.Sum256([]byte(m.flows[status.ID].verifier))
			m.mu.Unlock()
			if u.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(expected[:]) {
				t.Fatal("incorrect PKCE challenge")
			}
			if _, ok := m.Get("another-owner", 1, status.ID); ok {
				t.Fatal("login crossed owner boundary")
			}
			if _, err := m.Start("owner", 1, config); !errors.Is(err, ErrBusy) {
				t.Fatal("concurrent login accepted")
			}
			callback := status.Redirect + "?" + url.Values{"state": {u.Query().Get("state")}, "code": {"test-code"}}.Encode()
			bad, err := http.Get(status.Redirect + "?state=bad&code=test-code")
			if err != nil {
				t.Fatal(err)
			}
			bad.Body.Close()
			if bad.StatusCode != 400 {
				t.Fatal("invalid state accepted")
			}
			response, err := http.Get(callback)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal("callback failed")
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("exchange not started")
			}
			duplicate, err := http.Get(callback)
			if err != nil {
				t.Fatal(err)
			}
			duplicate.Body.Close()
			if duplicate.StatusCode != 409 {
				t.Fatal("callback replay accepted")
			}
			if outcome == "cancel" {
				if !m.Cancel("owner", 1, status.ID) {
					t.Fatal("cancel failed")
				}
			}
			if outcome == "timeout" {
				m.mu.Lock()
				m.flows[status.ID].cancel()
				m.mu.Unlock()
			}
			close(release)
			deadline := time.Now().Add(time.Second)
			for {
				current, _ := m.Get("owner", 1, status.ID)
				if current.Status != "pending" && current.Status != "exchanging" {
					want := map[string]string{"success": "succeeded", "cancel": "cancelled", "timeout": "expired", "save_error": "failed"}[outcome]
					if current.Status != want || current.URL != "" {
						t.Fatalf("unexpected result: %+v", current)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("login did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			if exchanges.Load() != 1 || (outcome == "cancel" || outcome == "timeout") && saves.Load() != 0 {
				t.Fatal("cancelled login saved or code exchanged twice")
			}
		})
	}
}

func TestManualCallbackValidationAndSingleExchange(t *testing.T) {
	m := NewManager()
	defer m.CancelOwner("")
	started, release := make(chan struct{}), make(chan struct{})
	var exchanges atomic.Int32
	login, err := m.Start("owner", 7, Config{
		Redirect: "http://127.0.0.1:0/callback", Issuer: "https://accounts.google.com",
		Authorize: func(state, challenge, redirect string) string {
			return "https://example.com/?" + url.Values{"state": {state}}.Encode()
		},
		Exchange: func(ctx context.Context, code, verifier, redirect string) (func() (int64, error), error) {
			if exchanges.Add(1) == 1 {
				close(started)
			}
			if code != "test-code" {
				t.Error("incorrect authorization code")
			}
			<-release
			return func() (int64, error) { return 9, nil }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer close(release)
	authorize, _ := url.Parse(login.URL)
	query := url.Values{"state": {authorize.Query().Get("state")}, "code": {"test-code"}, "iss": {"https://accounts.google.com"}}
	callback := login.Redirect + "?" + query.Encode()
	if _, err := m.Submit("other-owner", 7, login.ID, callback); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-owner submission accepted")
	}
	if _, err := m.Submit("owner", 8, login.ID, callback); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-provider submission accepted")
	}
	wrongState := url.Values{"state": {"wrong-state"}, "code": {"test-code"}}
	for _, invalid := range []string{
		"http://example.com/callback?" + query.Encode(),
		strings.Replace(callback, "http://", "https://", 1),
		strings.Replace(callback, "/callback?", "/other?", 1),
		login.Redirect + "?" + wrongState.Encode(),
		callback + "&code=another-code",
		callback + "&state=another-state",
		callback + "&error=access_denied",
		callback + "#fragment",
		login.Redirect + "?state=%xx&code=test-code",
		strings.Replace(callback, url.QueryEscape("https://accounts.google.com"), url.QueryEscape("https://example.com"), 1),
	} {
		if _, err := m.Submit("owner", 7, login.ID, invalid); err == nil || strings.Contains(err.Error(), "test-code") {
			t.Fatalf("invalid callback accepted or echoed: %v", err)
		}
	}
	if exchanges.Load() != 0 {
		t.Fatal("invalid callback started an exchange")
	}
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		_, err := m.Submit("owner", 7, login.ID, callback)
		if err != nil && !errors.Is(err, ErrAlreadyHandled) {
			t.Error(err)
		}
	}()
	go func() {
		defer group.Done()
		response, err := http.Get(callback)
		if err != nil {
			t.Error(err)
			return
		}
		response.Body.Close()
		if response.StatusCode != 200 && response.StatusCode != 409 {
			t.Errorf("automatic callback returned %d", response.StatusCode)
		}
	}()
	group.Wait()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("exchange not started")
	}
	if exchanges.Load() != 1 {
		t.Fatal("callback exchanged more than once")
	}
	if _, err := m.Submit("owner", 7, login.ID, callback); !errors.Is(err, ErrAlreadyHandled) {
		t.Fatal("callback replay accepted")
	}
}

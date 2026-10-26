package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/model-confluence/internal/antigravity"
	"github.com/Sanjeever/model-confluence/internal/oauthlogin"
	"github.com/Sanjeever/model-confluence/internal/store"
)

func TestBrowserOAuthAccountCreation(t *testing.T) {
	for _, kind := range []string{store.ProviderCodexOAuth, store.ProviderAntigravityOAuth} {
		t.Run(kind, func(t *testing.T) {
			s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			provider, err := s.CreateProvider(store.CreateProviderInput{Name: kind, Kind: kind})
			if err != nil {
				t.Fatal(err)
			}
			var auth struct {
				Tokens json.RawMessage `json:"tokens"`
			}
			if err := json.Unmarshal(gatewayAuthFile(t, "login-user", time.Now().Add(time.Hour)), &auth); err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/oauth/token", "/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("code") != "login-code" || len(r.Form.Get("code_verifier")) != 43 || r.Form.Get("grant_type") != "authorization_code" {
						t.Error("invalid authorization exchange")
					}
					if kind == store.ProviderCodexOAuth {
						w.Write(auth.Tokens)
					} else {
						io.WriteString(w, `{"access_token":"google-access","refresh_token":"google-refresh","expires_in":3600}`)
					}
				case "/oauth2/v2/userinfo":
					io.WriteString(w, `{"id":"google-user","email":"test@example.com","verified_email":true}`)
				case "/v1internal:loadCodeAssist":
					io.WriteString(w, `{"cloudaicompanionProject":"project"}`)
				default:
					t.Errorf("unexpected OAuth request: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer upstream.Close()
			target, _ := url.Parse(upstream.URL)
			transport := upstream.Client().Transport
			client := &http.Client{Transport: codexTestTransport(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				u := *r.URL
				copy.URL = &u
				copy.URL.Scheme, copy.URL.Host = target.Scheme, target.Host
				return transport.RoundTrip(copy)
			})}
			h := &Handler{store: s, client: client, antigravityOAuth: antigravity.OAuthClient{ID: "test-client", Secret: "test-secret"}}
			config, err := h.OAuthLogin(provider.ID)
			if err != nil {
				t.Fatal(err)
			}
			config.Redirect = "http://127.0.0.1:0/callback"
			manager := oauthlogin.NewManager()
			defer manager.CancelOwner("")
			login, err := manager.Start("owner", provider.ID, config)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(login.URL)
			if u.Query().Get("redirect_uri") != login.Redirect || u.Query().Get("code_challenge_method") != "S256" {
				t.Fatal("invalid authorization URL")
			}
			callback := login.Redirect + "?" + url.Values{"code": {"login-code"}, "state": {u.Query().Get("state")}, "iss": {config.Issuer}}.Encode()
			if kind == store.ProviderAntigravityOAuth {
				if _, err := manager.Submit("owner", provider.ID, login.ID, callback); err != nil {
					t.Fatal(err)
				}
			} else {
				response, err := http.Get(callback)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				status, _ := manager.Get("owner", provider.ID, login.ID)
				if status.Status == "succeeded" {
					break
				}
				if status.Status == "failed" || time.Now().After(deadline) {
					t.Fatalf("login failed: %+v", status)
				}
				time.Sleep(time.Millisecond)
			}
			providers, err := s.ListProviders()
			if err != nil || len(providers[0].Keys) != 1 {
				t.Fatalf("account missing: %+v %v", providers, err)
			}
			visible, _ := json.Marshal(providers)
			if strings.Contains(string(visible), "google-access") || strings.Contains(string(visible), "google-refresh") || strings.Contains(string(visible), "test-refresh") {
				t.Fatal("tokens exposed in account metadata")
			}
		})
	}
}

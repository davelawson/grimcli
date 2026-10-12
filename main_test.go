package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davelawson/grim/api"
)

func invoke(args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	code := run(context.Background(), args, &out, &diagnostics)
	return code, out.String(), diagnostics.String()
}

func TestCommands(t *testing.T) {
	isolatedDirectory(t)
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case api.Login.Route:
			if r.Method != api.Login.Method || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
				t.Errorf("incorrect login request")
			}
			var login api.LoginRequest
			if err := json.NewDecoder(r.Body).Decode(&login); err != nil || login.Email != "alice@example.com" || login.Password != "secret" {
				t.Errorf("incorrect login payload: %+v, %v", login, err)
			}
			json.NewEncoder(w).Encode(api.LoginResponse{Token: "alice-token"})
		case "/match/a/b":
			if r.Method != api.GetMatch.Method || r.URL.EscapedPath() != "/match/a%2Fb" {
				t.Errorf("incorrect match route: %s", r.URL)
			}
			tokens = append(tokens, r.Header.Get("Authorization"))
			json.NewEncoder(w).Encode(api.MatchResponse{Match: &api.MatchView{ID: "a/b"}})
		case "/match/id/catalogue":
			if r.Method != api.GetCatalogue.Method || r.Header.Get("Authorization") != "catalogue-token" {
				t.Errorf("incorrect catalogue request")
			}
			json.NewEncoder(w).Encode(api.CatalogueResponse{Catalogue: &api.Catalogue{RulesVersion: "opening-v1"}})
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	if code, out, err := invoke("login", "--server", server.URL, "--email", "alice@example.com", "--password", "secret"); code != 0 || out != "alice-token\n" || err != "" {
		t.Fatalf("login: %d %q %q", code, out, err)
	}
	for _, token := range []string{"alice-token", "bob-token"} {
		if code, out, err := invoke("match", "--server", server.URL, "--token", token, "--id", "a/b"); code != 0 || !strings.Contains(out, "\n  \"match\":") || err != "" {
			t.Fatalf("match: %d %q %q", code, out, err)
		}
	}
	if len(tokens) != 2 || tokens[0] != "alice-token" || tokens[1] != "bob-token" {
		t.Fatalf("tokens: %v", tokens)
	}
	if code, out, err := invoke("catalogue", "--server", server.URL, "--token", "catalogue-token", "--id", "id"); code != 0 || !strings.Contains(out, "opening-v1") || err != "" {
		t.Fatalf("catalogue: %d %q %q", code, out, err)
	}
}

func TestArgumentValidation(t *testing.T) {
	isolatedDirectory(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	for _, args := range [][]string{
		{}, {"unknown"}, {"match", "--server", server.URL, "--id", "id"},
		{"match", "--server", server.URL, "--token", "token"},
		{"login", "--server", server.URL, "--email", "alice"},
		{"match", "--server", "invalid", "--token", "token", "--id", "id"},
		{"match", "--server", server.URL, "--token", "token", "--id", "id", "extra"},
	} {
		if code, out, _ := invoke(args...); code != 2 || out != "" {
			t.Errorf("%v: code %d, output %q", args, code, out)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid arguments made %d requests", calls)
	}
	for _, args := range [][]string{{"--help"}, {"match", "--help"}} {
		if code, _, _ := invoke(args...); code != 0 {
			t.Errorf("help failed")
		}
	}
}

func TestResponseFailures(t *testing.T) {
	isolatedDirectory(t)
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"API error", 401, `{"error":{"code":"unauthorized","message":"invalid token"}}`, "HTTP 401: unauthorized: invalid token"},
		{"HTML error", 502, "<html>private upstream details</html>", "HTTP 502: unexpected error response"},
		{"malformed JSON", 200, "{", "decode response"},
		{"missing match", 200, "{}", "missing match"},
		{"trailing JSON", 200, `{"match":{}} {}`, "trailing JSON"},
		{"redirect", 302, "", "HTTP 302"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			code, out, err := invoke("match", "--server", server.URL, "--token", "token", "--id", "id")
			if code != 1 || out != "" || !strings.Contains(err, tc.want) || strings.Contains(err, "private upstream details") || calls != 1 {
				t.Fatalf("%d %q %q; calls %d", code, out, err, calls)
			}
		})
	}
}

func TestTLSAndTimeout(t *testing.T) {
	isolatedDirectory(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"match":{"id":"id"}}`)) }))
	defer server.Close()
	cert := filepath.Join(t.TempDir(), "server.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, err := invoke("match", "--server", server.URL, "--token", "token", "--id", "id", "--cacert", cert); code != 0 {
		t.Fatal(err)
	}
	if code, _, _ := invoke("match", "--server", server.URL, "--token", "token", "--id", "id"); code != 1 {
		t.Fatal("untrusted certificate accepted")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.pem")
	os.WriteFile(invalid, []byte("invalid"), 0600)
	for _, path := range []string{invalid, invalid + ".missing"} {
		if _, err := newHTTPClient(path); err == nil {
			t.Fatal("invalid certificate accepted")
		}
	}
	client, err := newHTTPClient("")
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 15*time.Second {
		t.Fatalf("timeout: %v", client.Timeout)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	client.Timeout = 20 * time.Millisecond
	err = execute(context.Background(), client, options{command: "match", server: slow.URL, token: "token", id: "id"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("timeout: %v", err)
	}
}

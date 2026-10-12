package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestLobbyCommands(t *testing.T) {
	isolatedDirectory(t)
	id, user := "lobby/a ?#%", "user/b ?#%"
	for _, tc := range []struct {
		command, method, path, body, response string
		status                                int
		flags                                 []string
	}{
		{"public-lobbies", "GET", "/lobbies/public", "", `{"lobbies":[]}`, 200, nil},
		{"lobby", "GET", "/lobby/" + url.PathEscape(id), "", `{"lobby":{"id":"lobby-id"}}`, 200, []string{"--id", id}},
		{"lobby-create", "POST", "/lobby", `{"name":"Friday game"}`, `{"id":"new-id"}`, 200, []string{"--name", "Friday game"}},
		{"lobby-join", "POST", "/lobby/" + url.PathEscape(id) + "/join", "", `{"lobby":{"id":"joined-id"}}`, 200, []string{"--id", id}},
		{"lobby-ready", "PUT", "/lobby/" + url.PathEscape(id) + "/ready", `{"ready":true}`, `{"lobby":{"id":"lobby-id"}}`, 200, []string{"--id", id, "--ready=true"}},
		{"lobby-ready", "PUT", "/lobby/" + url.PathEscape(id) + "/ready", `{"ready":false}`, `{"lobby":{"id":"lobby-id"}}`, 200, []string{"--id", id, "--ready=false"}},
		{"lobby-launch", "POST", "/lobby/" + url.PathEscape(id) + "/launch", `{"requestid":"` + id + `"}`, `{"match":{"id":"match-id"}}`, 201, []string{"--id", id}},
		{"lobby-leave", "DELETE", "/lobby/" + url.PathEscape(id) + "/leave", "", "", 200, []string{"--id", id}},
		{"lobby-remove-user", "DELETE", "/lobby/" + url.PathEscape(id) + "/user/" + url.PathEscape(user), "", "", 200, []string{"--id", id, "--userid", user}},
		{"lobby-delete", "DELETE", "/lobby/" + url.PathEscape(id), "", "", 200, []string{"--id", id}},
	} {
		t.Run(tc.command+tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.EscapedPath() != tc.path || r.Header.Get("Authorization") != "raw-token" || r.Header.Get("Accept") != "application/json" {
					t.Errorf("wrong request: %s %s %v", r.Method, r.URL, r.Header)
				}
				data, _ := io.ReadAll(r.Body)
				if string(data) != tc.body {
					t.Errorf("body: %s want %s", data, tc.body)
				}
				if (tc.body != "") != (r.Header.Get("Content-Type") == "application/json") {
					t.Error("content type")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.response)
			}))
			defer server.Close()
			args := append([]string{tc.command, "--server", server.URL, "--token", "raw-token"}, tc.flags...)
			code, out, diagnostics := invoke(args...)
			if code != 0 || diagnostics != "" {
				t.Fatalf("%d %s", code, diagnostics)
			}
			if tc.response == "" {
				if out != "" {
					t.Fatal(out)
				}
				return
			}
			if !strings.Contains(out, "\n  ") {
				t.Fatal("JSON is not indented")
			}
			var got, want map[string]any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			json.Unmarshal([]byte(tc.response), &want)
			for key, val := range want { // Typed envelopes may include additional zero-value fields.
				if key == "lobbies" || key == "id" {
					if !reflect.DeepEqual(got[key], val) {
						t.Fatal(got)
					}
				}
			}
		})
	}
}

func TestLobbyArgumentValidation(t *testing.T) {
	isolatedDirectory(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	for _, flags := range [][]string{
		{"lobby"}, {"lobby-create"}, {"lobby-create", "--name", " "}, {"lobby-join"}, {"lobby-ready", "--id", "id"},
		{"lobby-ready", "--id", "id", "--ready"}, {"lobby-ready", "--id", "id", "--ready=maybe"}, {"lobby-ready", "--id", "id", "--ready=1"},
		{"lobby-launch"}, {"lobby-leave"}, {"lobby-delete"}, {"lobby-remove-user", "--id", "id"}, {"lobby-remove-user", "--userid", "user"},
		{"public-lobbies", "--id", "id"}, {"lobby-leave", "--id", "id", "--userid", "user"},
	} {
		args := append(flags, "--server", server.URL, "--token", "token")
		if code, out, _ := invoke(args...); code != 2 || out != "" {
			t.Errorf("accepted %q", args)
		}
	}
	for _, command := range []string{"public-lobbies", "lobby", "lobby-create", "lobby-join", "lobby-ready", "lobby-launch", "lobby-leave", "lobby-remove-user", "lobby-delete"} {
		if code, _, _ := invoke(command, "--server", server.URL); code != 2 {
			t.Error("missing token accepted")
		}
		if code, _, _ := invoke(command, "--help"); code != 0 {
			t.Error("help failed")
		}
	}
	if calls != 0 {
		t.Fatal("invalid args made requests")
	}
}

func TestLobbyResponseFailures(t *testing.T) {
	isolatedDirectory(t)
	for _, tc := range []struct {
		command string
		status  int
		body    string
	}{
		{"lobby-create", 200, `{}`}, {"lobby", 200, `{}`}, {"lobby", 200, `{"lobby":null}`}, {"lobby-join", 200, `{"lobby":{}}`},
		{"lobby-ready", 200, `{"lobby":{"id":"id"}} {}`}, {"lobby-launch", 201, `{"match":{}}`}, {"lobby-launch", 200, `{"match":{"id":"id"}}`},
		{"public-lobbies", 200, `{}`}, {"public-lobbies", 200, `{"lobbies":[{}]}`}, {"lobby", 200, `{`},
		{"lobby-leave", 409, `{"error":{"code":"owner_must_transfer","message":"Transfer ownership"}}`},
		{"lobby-delete", 404, "legacy text error"}, {"lobby-remove-user", 403, `{"error":{"code":"forbidden","message":"Denied"}}`},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
		args := []string{tc.command, "--server", server.URL, "--token", "token"}
		if requiresID(tc.command) {
			args = append(args, "--id", "id")
		}
		switch tc.command {
		case "lobby-create":
			args = append(args, "--name", "name")
		case "lobby-ready":
			args = append(args, "--ready=false")
		case "lobby-remove-user":
			args = append(args, "--userid", "user")
		}
		code, out, diagnostics := invoke(args...)
		server.Close()
		if code != 1 || out != "" {
			t.Errorf("accepted %s %s", tc.command, tc.body)
		}
		if tc.status == 409 && !strings.Contains(diagnostics, "owner_must_transfer: Transfer ownership") {
			t.Error(diagnostics)
		}
	}
}

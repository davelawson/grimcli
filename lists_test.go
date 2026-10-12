package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/davelawson/grim/api"
)

func TestListCommands(t *testing.T) {
	for _, command := range []string{"matches", "lobbies"} {
		for _, empty := range []bool{false, true} {
			t.Run(command+map[bool]string{false: "-populated", true: "-empty"}[empty], func(t *testing.T) {
				isolatedDirectory(t)
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					wantPath := api.ListMatches.Route
					if command == "lobbies" {
						wantPath = api.ListLobbies.Route
					}
					if r.Method != http.MethodGet || r.URL.Path != wantPath || r.Header.Get("Authorization") != "per-command-token" {
						t.Error("wrong listing request")
					}
					entries := []map[string]string{}
					if !empty {
						entries = append(entries, map[string]string{"id": "id", "status": "setup"})
					}
					json.NewEncoder(w).Encode(map[string]any{command: entries})
				}))
				defer server.Close()
				data, _ := json.Marshal(config{Server: server.URL, Email: "ignored", Password: "ignored"})
				writeConfig(t, "grimcli.json", string(data))
				code, out, diagnostics := invoke(command, "--token", "per-command-token")
				if code != 0 || diagnostics != "" {
					t.Fatalf("listing: %d %s", code, diagnostics)
				}
				var envelope map[string][]map[string]string
				if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope[command] == nil {
					t.Fatalf("bad output: %s", out)
				}
				if code, _, _ := invoke(command); code != 2 {
					t.Error("missing token accepted")
				}
				if code, _, _ := invoke(command, "--token", "token", "--id", "id"); code != 2 {
					t.Error("id accepted for listing")
				}
				if calls != 1 {
					t.Fatalf("invalid commands issued requests: %d", calls)
				}
			})
		}
	}
}

func TestListResponseValidation(t *testing.T) {
	isolatedDirectory(t)
	for _, command := range []string{"matches", "lobbies"} {
		for _, body := range []string{`{}`, `{"matches":null,"lobbies":null}`} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			code, _, _ := invoke(command, "--server", server.URL, "--token", "token")
			server.Close()
			if code != 1 {
				t.Fatal("missing array accepted")
			}
		}
	}
}

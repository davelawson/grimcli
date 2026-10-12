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

	"github.com/davelawson/grim/api"
)

const draftUUID = "12345678-1234-5678-9abc-123456789abc"
const draftInitial = `{"draft":{"step":"chantry"},"cards":[{"id":"chantry_fire","name":"Fire Chantry","placeholder":true,"income":{"fire":2}}],"revision":0,"status":"setup"}`

func draftPickFlags() []string {
	return []string{"--type", "choose_chantry", "--cardid", "chantry_fire", "--revision", "0", "--requestid", draftUUID}
}

func TestDraftCommands(t *testing.T) {
	isolatedDirectory(t)
	id := "match/a ?#%"
	for _, command := range []string{"draft", "draft-pick"} {
		t.Run(command, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				method := "GET"
				if command == "draft-pick" {
					method = "POST"
				}
				if r.Method != method || r.URL.EscapedPath() != "/match/"+url.PathEscape(id)+"/draft" || r.Header.Get("Authorization") != "private-token" || r.Header.Get("Accept") != "application/json" {
					t.Errorf("incorrect request: %s %s %v", r.Method, r.URL, r.Header)
				}
				body, _ := io.ReadAll(r.Body)
				if command == "draft" {
					if len(body) != 0 || r.Header.Get("Content-Type") != "" {
						t.Error("GET has body or content type")
					}
				} else {
					var got map[string]any
					json.Unmarshal(body, &got)
					want := map[string]any{"requestid": draftUUID, "expectedrevision": float64(0), "type": "choose_chantry", "cardid": "chantry_fire"}
					if !reflect.DeepEqual(got, want) || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("pick payload: %s", body)
					}
				}
				io.WriteString(w, draftInitial)
			}))
			defer server.Close()
			args := []string{command, "--server", server.URL, "--token", "private-token", "--id", id}
			if command == "draft-pick" {
				args = append(args, draftPickFlags()...)
			}
			code, out, diagnostics := invoke(args...)
			var response api.DraftResponse
			if code != 0 || diagnostics != "" || !strings.Contains(out, "\n  \"draft\":") || json.Unmarshal([]byte(out), &response) != nil || response.Revision != 0 || response.Draft.Step != "chantry" || len(response.Cards) != 1 || calls != 1 {
				t.Fatalf("result: %d %q %q; calls %d", code, out, diagnostics, calls)
			}
		})
	}
}

func TestDraftArguments(t *testing.T) {
	isolatedDirectory(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; io.WriteString(w, draftInitial) }))
	defer server.Close()
	base := []string{"draft-pick", "--server", server.URL, "--token", "token", "--id", "id"}
	valid := append(append([]string{}, base...), draftPickFlags()...)
	for _, flag := range []string{"id", "type", "cardid", "revision", "requestid"} {
		for _, value := range []string{"missing", ""} {
			args := []string{}
			for i := 0; i < len(valid); i++ {
				if valid[i] == "--"+flag {
					if value == "" {
						args = append(args, valid[i], "")
					}
					i++
					continue
				}
				args = append(args, valid[i])
			}
			if code, out, diag := invoke(args...); code != 2 || out != "" || diag == "" {
				t.Errorf("%s %s: %d %q %q", flag, value, code, out, diag)
			}
		}
	}
	for _, pair := range [][2]string{
		{"revision", "-1"}, {"revision", "abc"}, {"revision", "9223372036854775808"},
		{"type", "chantry"}, {"type", "choose_other"}, {"cardid", " \t"}, {"id", " \t"},
		{"requestid", "12345678123456789abc123456789abc"}, {"requestid", "urn:uuid:" + draftUUID},
		{"requestid", "{" + draftUUID + "}"}, {"requestid", "g2345678-1234-5678-9abc-123456789abc"},
	} {
		args := append([]string{}, valid...)
		for i := range args {
			if args[i] == "--"+pair[0] {
				args[i+1] = pair[1]
				break
			}
		}
		if code, out, _ := invoke(args...); code != 2 || out != "" {
			t.Errorf("%v accepted", pair)
		}
	}
	for _, args := range [][]string{
		{"draft", "--server", server.URL, "--id", "id"},
		{"draft", "--server", server.URL, "--token", "token"},
		{"draft-pick", "--server", server.URL, "--id", "id"},
	} {
		if code, _, _ := invoke(args...); code != 2 {
			t.Errorf("accepted %v", args)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid arguments issued %d requests", calls)
	}
	for _, kind := range []string{"choose_chantry", "choose_victory_path", "choose_minion"} {
		args := append(append([]string{}, valid...), "--type", kind, "--requestid", strings.ToUpper(draftUUID))
		if code, _, diag := invoke(args...); code != 0 {
			t.Errorf("valid choice rejected: %s", diag)
		}
	}
	for _, command := range []string{"draft", "draft-pick"} {
		if code, _, diag := invoke(command, "--help"); code != 0 || !strings.Contains(diag, "-token") {
			t.Fatalf("help: %d %s", code, diag)
		}
	}
}

func TestDraftResponseValidation(t *testing.T) {
	isolatedDirectory(t)
	malformed := []string{
		`{`, `null`, `{}`, `[]`, draftInitial + ` {}`,
		`{"draft":{},"cards":[],"revision":0,"status":"setup"}`,
		`{"draft":{"step":"other"},"cards":[],"revision":0,"status":"setup"}`,
	}
	for _, key := range []string{"draft", "cards", "revision", "status"} {
		for _, null := range []bool{false, true} {
			var body map[string]any
			json.Unmarshal([]byte(draftInitial), &body)
			if null {
				body[key] = nil
			} else {
				delete(body, key)
			}
			data, _ := json.Marshal(body)
			malformed = append(malformed, string(data))
		}
	}
	for _, pair := range []struct {
		key   string
		value any
	}{
		{"revision", -1}, {"revision", 0.5}, {"revision", "0"}, {"status", ""}, {"status", "other"},
		{"cards", []any{}}, {"cards", []any{nil}}, {"cards", []any{map[string]any{"id": "a"}}},
		{"cards", []any{map[string]any{"id": " ", "name": "x"}}},
		{"cards", []any{map[string]any{"id": "a", "name": "x"}, map[string]any{"id": "a", "name": "x"}}},
	} {
		var body map[string]any
		json.Unmarshal([]byte(draftInitial), &body)
		body[pair.key] = pair.value
		data, _ := json.Marshal(body)
		malformed = append(malformed, string(data))
	}
	for _, command := range []string{"draft", "draft-pick"} {
		for _, body := range malformed {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			args := []string{command, "--server", server.URL, "--token", "token", "--id", "id"}
			if command == "draft-pick" {
				args = append(args, draftPickFlags()...)
			}
			code, out, diag := invoke(args...)
			server.Close()
			if code != 1 || out != "" || diag == "" {
				t.Errorf("%s accepted %s: %d %q %q", command, body, code, out, diag)
			}
		}
	}
	for _, status := range []string{"setup", "playing"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"draft":{"step":"complete"},"cards":[],"revision":6,"status":"`+status+`"}`)
		}))
		code, _, diag := invoke("draft", "--server", server.URL, "--token", "token", "--id", "id")
		server.Close()
		if code != 0 {
			t.Errorf("completed draft rejected: %s", diag)
		}
	}
}

func TestDraftBackendErrors(t *testing.T) {
	isolatedDirectory(t)
	for _, command := range []string{"draft", "draft-pick"} {
		for _, tc := range []struct {
			status     int
			body, want string
		}{
			{409, `{"error":{"code":"stale_revision","message":"refresh the draft"}}`, "HTTP 409: stale_revision: refresh the draft"},
			{401, `{"error":{"code":"unauthorized","message":"invalid token"}}`, "HTTP 401: unauthorized: invalid token"},
			{502, "private upstream details", "HTTP 502: unexpected error response"},
		} {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			args := []string{command, "--server", server.URL, "--token", "token", "--id", "id"}
			if command == "draft-pick" {
				args = append(args, draftPickFlags()...)
			}
			code, out, diag := invoke(args...)
			server.Close()
			if code != 1 || out != "" || !strings.Contains(diag, tc.want) || calls != 1 {
				t.Fatalf("%d %q %q calls=%d", code, out, diag, calls)
			}
		}
	}
}

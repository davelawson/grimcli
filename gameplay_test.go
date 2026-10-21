package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func gameplayBase(command, server string) []string {
	return []string{command, "--server", server, "--token", "private-token", "--id", "match/a ?#%+", "--revision", "0", "--requestid", draftUUID}
}

type gameplayCase struct {
	command, path   string
	flags, required []string
	fields          map[string]any
}

func gameplayCases() []gameplayCase {
	return []gameplayCase{
		{"play-card", "/actions/play-card", []string{"--cardid", "cardid/ + ?#%雪", "--target", "first / +雪", "--target", "last\n$(touch injected)", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)", "--discard", "first / +雪", "--discard", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "targets": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "discard": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"attack", "/actions/attack", []string{"--cardid", "cardid/ + ?#%雪", "--minion", "first / +雪", "--minion", "last\n$(touch injected)", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid", "minion"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "minions": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"challenge", "/actions/attempt-challenge", []string{"--cardid", "cardid/ + ?#%雪", "--challengeid", "challengeid/ + ?#%雪", "--minion", "first / +雪", "--minion", "last\n$(touch injected)", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid", "challengeid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "challengeid": jsonValue("\"challengeid/ + ?#%雪\""), "minions": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"activate", "/actions/activate-ability", []string{"--cardid", "cardid/ + ?#%雪", "--abilityid", "abilityid/ + ?#%雪", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid", "abilityid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "abilityid": jsonValue("\"abilityid/ + ?#%雪\""), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"research-element", "/actions/research-element", []string{"--cardid", "cardid/ + ?#%雪", "--element", "fire"}, []string{"id", "revision", "requestid", "cardid", "element"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "element": jsonValue("\"fire\"")}},
		{"research-spell", "/actions/research-spell", []string{"--cardid", "cardid/ + ?#%雪", "--element-card", "first / +雪", "--element-card", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "elements": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"heal-with-focus", "/actions/heal-with-focus", []string{"--cardid", "cardid/ + ?#%雪", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"end-turn", "/actions/end-turn", []string{}, []string{"id", "revision", "requestid"}, map[string]any{}},
		{"maintenance", "/choices/CHOICE/maintenance", []string{"--choiceid", "choice/:id + ?#%雪", "--keep", "first / +雪", "--keep", "last\n$(touch injected)", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "choiceid"}, map[string]any{"keep": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"kneel", "/choices/CHOICE/kneel", []string{"--choiceid", "choice/:id + ?#%雪", "--kneel=false"}, []string{"id", "revision", "requestid", "choiceid", "kneel"}, map[string]any{"kneel": jsonValue("false")}},
		{"resolve-effect", "/choices/CHOICE/resolve-effect", []string{"--choiceid", "choice/:id + ?#%雪", "--effectid", "effectid/ + ?#%雪"}, []string{"id", "revision", "requestid", "choiceid", "effectid"}, map[string]any{"effectid": jsonValue("\"effectid/ + ?#%雪\"")}},
		{"skip-effect", "/choices/CHOICE/skip-effect", []string{"--choiceid", "choice/:id + ?#%雪", "--effectid", "effectid/ + ?#%雪"}, []string{"id", "revision", "requestid", "choiceid", "effectid"}, map[string]any{"effectid": jsonValue("\"effectid/ + ?#%雪\"")}},
		{"undertake-hunt", "/choices/CHOICE/undertake-hunt", []string{"--choiceid", "choice/:id + ?#%雪", "--effectid", "effectid/ + ?#%雪", "--minion", "first / +雪", "--minion", "last\n$(touch injected)", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "choiceid", "effectid"}, map[string]any{"effectid": jsonValue("\"effectid/ + ?#%雪\""), "minions": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"choose-research", "/choices/CHOICE/research", []string{"--choiceid", "choice/:id + ?#%雪", "--designid", "designid/ + ?#%雪"}, []string{"id", "revision", "requestid", "choiceid", "designid"}, map[string]any{"designid": jsonValue("\"designid/ + ?#%雪\"")}},
		{"end-discard", "/choices/CHOICE/end-discard", []string{"--choiceid", "choice/:id + ?#%雪", "--discard", "first / +雪", "--discard", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "choiceid"}, map[string]any{"cards": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"hand-discard", "/choices/CHOICE/hand-discard", []string{"--choiceid", "choice/:id + ?#%雪", "--cardid", "cardid/ + ?#%雪", "--disposition", "destroy"}, []string{"id", "revision", "requestid", "choiceid", "cardid", "disposition"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "disposition": jsonValue("\"destroy\"")}},
		{"asset-loss", "/choices/CHOICE/asset-loss", []string{"--choiceid", "choice/:id + ?#%雪", "--cardid", "cardid/ + ?#%雪"}, []string{"id", "revision", "requestid", "choiceid", "cardid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\"")}},
		{"scry", "/choices/CHOICE/scry", []string{"--choiceid", "choice/:id + ?#%雪", "--take", "take/ + ?#%雪", "--order", "first / +雪", "--order", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "choiceid", "take"}, map[string]any{"take": jsonValue("\"take/ + ?#%雪\""), "order": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"react", "/reactions/play-card", []string{"--cardid", "cardid/ + ?#%雪", "--target", "first / +雪", "--target", "last\n$(touch injected)", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water", "--payment", "first / +雪", "--payment", "last\n$(touch injected)"}, []string{"id", "revision", "requestid", "cardid"}, map[string]any{"cardid": jsonValue("\"cardid/ + ?#%雪\""), "targets": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]"), "payment": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]")}},
		{"defend", "/reactions/defend", []string{"--minion", "first / +雪", "--minion", "last\n$(touch injected)", "--support", "support=one", "--support-domain", "support=one=fire", "--support-domain", "support=one=water"}, []string{"id", "revision", "requestid"}, map[string]any{"minions": jsonValue("[\"first / +雪\", \"last\\n$(touch injected)\"]"), "support": jsonValue("[{\"cardid\": \"support=one\", \"domains\": [\"fire\", \"water\"]}]")}},
		{"pass", "/reactions/pass", []string{}, []string{"id", "revision", "requestid"}, map[string]any{}},
	}
}
func jsonValue(s string) any { var value any; _ = json.Unmarshal([]byte(s), &value); return value }
func TestGameplayCommands(t *testing.T) {
	isolatedDirectory(t)
	for _, tc := range gameplayCases() {
		t.Run(tc.command, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				path := "/match/" + url.PathEscape("match/a ?#%+") + strings.Replace(tc.path, "CHOICE", url.PathEscape("choice/:id + ?#%雪"), 1)
				if r.Method != "POST" || r.URL.EscapedPath() != path || r.Header.Get("Authorization") != "private-token" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("request %s %s %v", r.Method, r.URL, r.Header)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				want := map[string]any{"requestid": draftUUID, "expectedrevision": float64(0)}
				for k, v := range tc.fields {
					want[k] = v
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("payload got %#v want %#v", got, want)
				}
				io.WriteString(w, `{"match":{"id":"m","revision":1,"status":"playing","you":{"userid":"me","hand":[]},"choice":{"id":"private","type":"scry","options":["secret"]},"exchange":{"priority":"me","stack":[{"id":"spell","spell":true}]}}}`)
			}))
			defer server.Close()
			code, out, diag := invoke(append(gameplayBase(tc.command, server.URL), tc.flags...)...)
			if code != 0 || diag != "" || calls != 1 || !strings.Contains(out, `"secret"`) || !strings.Contains(out, `"spell": true`) {
				t.Fatalf("%d %s %s calls=%d", code, out, diag, calls)
			}
		})
	}
}
func TestGameplayArgumentValidation(t *testing.T) {
	isolatedDirectory(t)
	for _, tc := range gameplayCases() {
		t.Run(tc.command, func(t *testing.T) {
			valid := append(gameplayBase(tc.command, "http://localhost:1"), tc.flags...)
			for _, name := range tc.required {
				for _, missing := range []bool{false, true} {
					args := []string{}
					for i := 0; i < len(valid); i++ {
						if valid[i] == "--"+name || strings.HasPrefix(valid[i], "--"+name+"=") {
							if !missing {
								args = append(args, "--"+name, "")
							}
							if valid[i] == "--"+name {
								i++
							}
							continue
						}
						args = append(args, valid[i])
					}
					code, out, diag := invoke(args...)
					if code != 2 || out != "" || !strings.Contains(diag, name) {
						t.Errorf("%s: %d %q %q", name, code, out, diag)
					}
				}
			}
			for _, bad := range [][]string{{"--revision", "-1"}, {"--revision", "99999999999999999999"}, {"--requestid", "bad"}, {"--type", "pass"}, {"--mode", "heal"}, {"--selection", "x"}, {"extra"}} {
				code, _, _ := invoke(append(append([]string{}, valid...), bad...)...)
				if code != 2 {
					t.Errorf("accepted %v", bad)
				}
			}
		})
	}
	for _, flags := range [][]string{{"--support-domain", "broken"}, {"--support-domain", "a="}, {"--support-domain", "=air"}, {"--support-domain", "a=air"}, {"--support", "a", "--support", "a"}, {"--target", " "}, {"--support", ""}, {"--minion", "m"}, {"--element", "fire"}} {
		args := append(gameplayBase("play-card", "http://localhost:1"), "--cardid", "c")
		code, _, _ := invoke(append(args, flags...)...)
		if code != 2 {
			t.Errorf("accepted %v", flags)
		}
	}
	for _, tc := range []struct{ command, flag string }{{"pass", "cardid"}, {"end-turn", "payment"}, {"activate", "target"}, {"react", "discard"}, {"react", "minion"}, {"defend", "payment"}, {"challenge", "target"}, {"maintenance", "cardid"}, {"research-spell", "payment"}, {"research-element", "payment"}, {"resolve-effect", "minion"}, {"skip-effect", "payment"}, {"heal-with-focus", "target"}} {
		var args []string
		for _, sample := range gameplayCases() {
			if sample.command == tc.command {
				args = append(gameplayBase(tc.command, "http://localhost:1"), sample.flags...)
			}
		}
		code, _, _ := invoke(append(args, "--"+tc.flag, "value")...)
		if code != 2 {
			t.Errorf("accepted %s --%s", tc.command, tc.flag)
		}
	}
	for _, bad := range [][]string{{"kneel", "--choiceid", "q", "--kneel"}, {"kneel", "--choiceid", "q", "--kneel=maybe"}, {"hand-discard", "--choiceid", "q", "--cardid", "c", "--disposition", "other"}} {
		args := append(gameplayBase(bad[0], "http://localhost:1"), bad[1:]...)
		code, _, _ := invoke(args...)
		if code != 2 {
			t.Errorf("accepted %v", bad)
		}
	}
	code, _, _ := invoke(gameplayBase("choose", "http://localhost:1")...)
	if code != 2 {
		t.Fatal("generic choose retained")
	}
}
func TestGameplayErrorsAndResponses(t *testing.T) {
	isolatedDirectory(t)
	for _, sample := range gameplayCases() {
		for _, tc := range []struct {
			status           int
			body, diagnostic string
		}{
			{409, `{"error":{"code":"stale_revision","message":"refresh"}}`, "stale_revision"}, {403, `{"error":{"code":"forbidden","message":"private"}}`, "forbidden"}, {500, "bad", "HTTP 500"}, {200, `{}`, "missing match"}, {200, `{"match":null}`, "missing match"}, {200, `{"match":{"id":"m"}} {}`, "trailing JSON"}, {200, `{"match":`, "decode response"},
		} {
			t.Run(fmt.Sprintf("%s/%d/%s", sample.command, tc.status, tc.diagnostic), func(t *testing.T) {
				calls := 0
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.body)
				}))
				defer s.Close()
				args := append(gameplayBase(sample.command, s.URL), sample.flags...)
				code, out, diag := invoke(args...)
				if code != 1 || out != "" || calls != 1 || !strings.Contains(diag, tc.diagnostic) {
					t.Fatalf("%d %q %q calls=%d", code, out, diag, calls)
				}
			})
		}
	}
}
func TestEmptyTypedSelections(t *testing.T) {
	for _, command := range []string{"maintenance", "end-discard", "research-spell", "scry"} {
		args := gameplayBase(command, "http://localhost:1")
		switch command {
		case "maintenance", "end-discard":
			args = append(args, "--choiceid", "q")
		case "research-spell":
			args = append(args, "--cardid", "r")
		case "scry":
			args = append(args, "--choiceid", "q", "--take", "c")
		}
		if _, _, err := parse(args, io.Discard); err != nil {
			t.Fatal(command, err)
		}
	}
}

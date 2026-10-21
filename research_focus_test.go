package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/davelawson/grim/api"
)

func TestResearchFocusCommands(t *testing.T) {
	isolatedDirectory(t)
	for _, command := range []string{"research-element", "research-spell"} {
		t.Run(command, func(t *testing.T) {
			focusID := "focus=one /雪 $(touch injected)"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/actions/"+command) || r.Header.Get("Authorization") != "private-token" {
					t.Error("wrong Research request", r.Method, r.URL)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				want := map[string]any{"requestid": draftUUID, "expectedrevision": float64(0), "cardid": "research", "support": []any{map[string]any{"cardid": focusID, "domains": []any{"water"}}}}
				if command == "research-element" {
					want["element"] = "earth"
				} else {
					want["elements"] = []any{"fire-instance", "air-instance"}
				}
				if !reflect.DeepEqual(body, want) {
					t.Errorf("got %#v want %#v", body, want)
				}
				io.WriteString(w, `{"match":{"id":"match-one"}}`)
			}))
			defer server.Close()
			args := append(gameplayBase(command, server.URL), "--cardid", "research", "--support", focusID, "--support-domain", focusID+"=water")
			if command == "research-element" {
				args = append(args, "--element", "earth")
			} else {
				args = append(args, "--element-card", "fire-instance", "--element-card", "air-instance")
			}
			if code, _, diagnostics := invoke(args...); code != 0 {
				t.Fatal(code, diagnostics)
			}
			base := append(gameplayBase(command, "http://localhost:1"), "--cardid", "research")
			if command == "research-element" {
				base = append(base, "--element", "earth")
			}
			for _, bad := range [][]string{{"--support", " "}, {"--support-domain", "focus=water"}, {"--support", "focus", "--support", "focus"}, {"--support", "focus", "--support-domain", "focus="}} {
				if code, _, _ := invoke(append(append([]string{}, base...), bad...)...); code != 2 {
					t.Errorf("accepted invalid flags %q", bad)
				}
			}
		})
	}
}

func TestGuidedResearchFocus(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
		status      int
	}{
		{"discover", "1\n1\n1\nd\n1\nd\n4\n", []string{"research-spell", "--cardid", "r", "--element-card", "e", "--support", "f", "--support-domain", "f=water"}, 0},
		{"learn", "1\n2\n3\n1\nd\n4\n", []string{"research-element", "--cardid", "r", "--element", "air", "--support", "f", "--support-domain", "f=water"}, 0},
		{"skip", "1\n1\nd\nd\n", []string{"research-spell", "--cardid", "r"}, 0},
		{"one focus maximum", "1\n1\nd\n1\n2\nd\n4\n", []string{"research-spell", "--cardid", "r", "--support", "f", "--support-domain", "f=water"}, 0},
		{"cancel Focus", "1\n1\nd\n0\n", nil, 1},
		{"escape Focus", "1\n2\n3\n\x1b\n", nil, 1},
		{"cancel Affinity", "1\n2\n3\n1\nd\n0\n", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := gameView()
			v.You.Hand = []api.CardInstance{gameCard("r", "research_spell"), gameCard("e", "element_fire"), gameCard("f", "arcane_focus"), gameCard("f2", "arcane_focus"), gameCard("k", "keen_insight")}
			w := v.Wizards["me"]
			w.Resources["wis"] = 0
			v.Wizards["me"] = w
			args, status, out := composeGame(t, v, "play-card", tc.input)
			if status != tc.status || (status == 0 && !reflect.DeepEqual(args, tc.want)) {
				t.Fatalf("status=%d args=%q\n%s", status, args, out)
			}
			if tc.name == "one focus maximum" && !strings.Contains(out, "Maximum reached") {
				t.Fatal(out)
			}
			if !strings.Contains(out, "provides 1 Wis") {
				t.Fatal("Focus payment guidance missing", out)
			}
		})
	}
}

func TestSessionResearchFocusReviewAndRetry(t *testing.T) {
	for _, send := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel review", true: "exact retry"}[send], func(t *testing.T) {
			v := gameView()
			v.You.Hand = []api.CardInstance{gameCard("r", "research_spell"), gameCard("f", "arcane_focus")}
			w := v.Wizards["me"]
			w.Resources["wis"] = 0
			v.Wizards["me"] = w
			before := gameResponse(t, v)
			v.Revision = 1
			after := gameResponse(t, v)
			tail := "0\nq\n"
			replies := map[string][]sessionReply{"match": {{Body: before}, {Body: before}}}
			if send {
				tail = "s\nr\nr\nq\n"
				replies["match"] = append(replies["match"], sessionReply{Failed: true}, sessionReply{Body: after})
				replies["research-spell"] = []sessionReply{{Failed: true}, {Body: after}}
			}
			f, cmd, out := gameplaySession(t, "4\n1\n1\n1\n1\n1\nd\n1\nd\n4\n"+tail, replies)
			runSession(t, cmd, out)
			expected := []string{"login", "matches", "match", "catalogue", "match"}
			if send {
				expected = append(expected, "research-spell", "research-spell", "match", "match")
			}
			calls := assertCommands(t, f, expected...)
			assertText(t, out, "Review submission")
			if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "--support f --support-domain f=water") {
				t.Fatal("review omitted Focus support arguments", out)
			}
			if send {
				if !reflect.DeepEqual(calls[5], calls[6]) || draftArg(t, calls[5], "revision") != "0" {
					t.Fatal("Research retry changed submission", calls)
				}
				assertText(t, out, "Submission accepted", "Match fetch failed")
			}
		})
	}
}

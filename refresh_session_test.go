package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func refreshList(kind string, ids ...string) string {
	key := "lobbies"
	if kind == "matches" {
		key = "matches"
	}
	entries := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, map[string]any{"id": id, "name": id, "status": "open", "membercount": 1})
	}
	data, _ := json.Marshal(map[string]any{key: entries})
	return string(data)
}

func TestSessionEmptyInputRefreshesLists(t *testing.T) {
	for _, tc := range []struct {
		kind, menu, selection string
	}{
		{"public-lobbies", "2", "lobby-join"},
		{"lobbies", "3", "lobby"},
		{"matches", "4", "match"},
	} {
		for _, empty := range []bool{false, true} {
			name := tc.kind + "/populated"
			if empty {
				name = tc.kind + "/empty"
			}
			t.Run(name, func(t *testing.T) {
				f := newSessionFixture(t, sessionConfig)
				before := refreshList(tc.kind, "old-first", "moved-first")
				if empty {
					before = refreshList(tc.kind)
				}
				// Whitespace stays invalid; only the following empty line refreshes.
				cmd, out := f.command(t, "\n\n"+tc.menu+"\n \n\n1\n0\n0\nq\n")
				f.replies(t, cmd, map[string][]sessionReply{tc.kind: {
					{Body: before}, {Body: refreshList(tc.kind, "moved-first", "new-second")},
				}})
				runSession(t, cmd, out)
				calls := assertCommands(t, f, "login", tc.kind, tc.kind, tc.selection)
				if !reflect.DeepEqual(calls[1], calls[2]) {
					t.Fatal("refresh changed request arguments", calls)
				}
				if got := draftArg(t, calls[3], "id"); got != "moved-first" {
					t.Fatalf("numbered selection used %q instead of refreshed list", got)
				}
				assertText(t, out, "new-second", "r) Refresh (Enter)", "Choose one of the listed numbers or actions")
				if empty {
					assertText(t, out, map[string]string{"public-lobbies": "No public lobbies", "lobbies": "No lobby memberships", "matches": "No matches"}[tc.kind])
				} else {
					assertText(t, out, "old-first")
				}
			})
		}
	}
}

func TestSessionEmptyInputRefreshesDetails(t *testing.T) {
	for _, tc := range []struct {
		name, menu, list, command, before, after, expected, afterInput string
		openMatch                                                      bool
	}{
		{
			name: "open lobby readiness", menu: "3", list: "lobbies", command: "lobby",
			before:   lobbyView("lobby-one", "open", ""),
			after:    strings.ReplaceAll(lobbyView("lobby-one", "open", ""), "false", "true"),
			expected: "Ready: 1/1", afterInput: "0\n0\nq\n",
		},
		{
			name: "open lobby becomes closed", menu: "3", list: "lobbies", command: "lobby",
			before: lobbyView("lobby-one", "open", ""), after: lobbyView("lobby-one", "closed", "refreshed-match"),
			expected: "1) Open match", afterInput: "1\n0\n0\nq\n", openMatch: true,
		},
		{
			name: "closed lobby gains match", menu: "3", list: "lobbies", command: "lobby",
			before: lobbyView("lobby-one", "closed", ""), after: lobbyView("lobby-one", "closed", "refreshed-match"),
			expected: "1) Open match", afterInput: "1\n0\n0\nq\n", openMatch: true,
		},
		{
			name: "other non-open lobby", menu: "3", list: "lobbies", command: "lobby",
			before: lobbyView("lobby-one", "deleted", ""), after: lobbyView("lobby-one", "unknown", ""),
			expected: "Status: unknown", afterInput: "0\n0\nq\n",
		},
		{
			name: "match starts", menu: "4", list: "matches", command: "match",
			before: matchView("match-one", "setup", "complete"), after: matchView("match-one", "playing", "complete"),
			expected: "Game started.", afterInput: "0\n0\nq\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, "\n\n"+tc.menu+"\n1\n \n\n"+tc.afterInput)
			f.replies(t, cmd, map[string][]sessionReply{tc.command: {{Body: tc.before}, {Body: tc.after}}})
			runSession(t, cmd, out)
			want := []string{"login", tc.list, tc.command, tc.command}
			if tc.openMatch {
				want = append(want, "match")
			}
			calls := assertCommands(t, f, want...)
			if !reflect.DeepEqual(calls[2], calls[3]) {
				t.Fatal("refresh changed request arguments", calls)
			}
			if tc.openMatch && draftArg(t, calls[4], "id") != "refreshed-match" {
				t.Fatal("Open match used stale lobby details", calls)
			}
			assertText(t, out, tc.expected, "Refresh (Enter)", "Choose one of the listed actions")
			if tc.command == "match" {
				assertText(t, out, "Active wizard: owner", "Turn: 2")
			}
		})
	}
}

func TestSessionEmptyInputRefreshFailures(t *testing.T) {
	for _, tc := range []struct {
		name, navigation, command, body, label, backExit string
		prefix                                           []string
	}{
		{"public lobbies", "2\n", "public-lobbies", refreshList("public-lobbies"), "List fetch", "q\n", []string{"login", "public-lobbies"}},
		{"my lobbies", "3\n", "lobbies", refreshList("lobbies"), "List fetch", "q\n", []string{"login", "lobbies"}},
		{"my matches", "4\n", "matches", refreshList("matches"), "List fetch", "q\n", []string{"login", "matches"}},
		{"open lobby", "3\n1\n", "lobby", lobbyView("lobby-one", "open", ""), "Lobby fetch", "0\nq\n", []string{"login", "lobbies", "lobby"}},
		{"closed lobby", "3\n1\n", "lobby", lobbyView("lobby-one", "closed", "match-one"), "Lobby fetch", "0\nq\n", []string{"login", "lobbies", "lobby"}},
		{"match", "4\n1\n", "match", matchView("match-one", "setup", "complete"), "Match fetch", "0\nq\n", []string{"login", "matches", "match"}},
	} {
		for _, retry := range []bool{false, true} {
			name := tc.name + "/back"
			if retry {
				name = tc.name + "/retry"
			}
			t.Run(name, func(t *testing.T) {
				f := newSessionFixture(t, sessionConfig)
				afterFailure := "0\n" + tc.backExit
				want := append(append([]string{}, tc.prefix...), tc.command)
				if retry {
					afterFailure = "r\n0\n" + tc.backExit
					want = append(want, tc.command)
				}
				// Empty input at the failure prompt must still ask for Retry/Back.
				cmd, out := f.command(t, "\n\n"+tc.navigation+"\n\n"+afterFailure)
				f.replies(t, cmd, map[string][]sessionReply{tc.command: {{Body: tc.body}, {Failed: true}, {Body: tc.body}}})
				runSession(t, cmd, out)
				calls := assertCommands(t, f, want...)
				initial := len(tc.prefix) - 1
				for _, call := range calls[initial+1:] {
					if !reflect.DeepEqual(calls[initial], call) {
						t.Fatal("refresh/retry changed request arguments", calls)
					}
				}
				assertText(t, out, tc.label+" failed. Retry (r) or Back (0):", "Choose Retry (r) or Back (0).")
			})
		}
	}
}

func TestSessionEmptyInputLeavesNestedMenusUnchanged(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n\n5\n\n0\n3\n1\n4\n\n2\n\n0\n0\n0\n0\n4\n1\n3\n\n0\n0\n0\nq\n")
	runSession(t, cmd, out)
	assertCommands(t, f, "login", "lobbies", "lobby", "matches", "match")
	assertText(t, out, "Choose one of the listed actions", "Choose one of the listed member numbers")
}

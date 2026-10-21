package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func botLobbyView(bots map[string]string, ready bool, status string) string {
	roster := map[string]any{}
	for id, name := range bots {
		roster[id] = map[string]string{"name": name}
	}
	data, _ := json.Marshal(map[string]any{"lobby": map[string]any{
		"id": "lobby-one", "name": "Friday", "owner": "owner", "status": status,
		"members": []string{"owner"}, "readiness": map[string]bool{"owner": ready},
		"bots": roster, "matchid": "match-one",
	}})
	return string(data)
}

func TestSessionAddBotReadyAndLaunch(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n3\n1\n4\n5\n1\n5\nq\n")
	bots := map[string]string{"bot-one": "Bot 1"}
	f.replies(t, cmd, map[string][]sessionReply{
		"lobby":         {{Body: botLobbyView(nil, true, "open")}},
		"lobby-add-bot": {{Body: botLobbyView(bots, false, "open")}},
		"lobby-ready":   {{Body: botLobbyView(bots, true, "open")}},
	})
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "lobbies", "lobby", "lobby-add-bot", "lobby-ready", "lobby-launch")
	for _, call := range calls[2:] {
		if draftArg(t, call, "id") != "lobby-one" {
			t.Fatal(call)
		}
	}
	assertText(t, out, "Players (2/4)", "Bot 1 [ID: bot-one] — bot, always ready", "Ready: 0/1 humans", "Human readiness has been reset", "Match launched")
}

func TestSessionRemoveBotUsesSortedRoster(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	botID := "bot-a $(touch injected) ; *"
	bots := map[string]string{"bot-z": "Bot 2", botID: "Bot 1"}
	cmd, out := f.command(t, "\n\n3\n1\n4\n6\nx\n01\n999999999999999999999\n1\nq\n")
	f.replies(t, cmd, map[string][]sessionReply{
		"lobby":            {{Body: botLobbyView(bots, true, "open")}},
		"lobby-remove-bot": {{Body: botLobbyView(map[string]string{"bot-z": "Bot 2"}, false, "open")}},
	})
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "lobbies", "lobby", "lobby-remove-bot")
	if draftArg(t, calls[3], "botid") != botID || draftArg(t, calls[3], "id") != "lobby-one" {
		t.Fatal(calls)
	}
	assertText(t, out, "1) Bot 1 [ID: "+botID+"]", "2) Bot 2 [ID: bot-z]", "Choose one of the listed bot numbers", "Ready: 0/1 humans")
	if _, err := os.Stat(filepath.Join(f.dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("bot ID was evaluated as shell code", err)
	}
}

func TestSessionBotMenuAvailabilityAndCancel(t *testing.T) {
	for _, tc := range []struct {
		name, status, input string
		bots                map[string]string
		add, remove         bool
	}{
		{"empty", "open", "4\n6\n0\nq\n", nil, true, false},
		{"full", "open", "4\n5\n0\nq\n", map[string]string{"bot-a": "Bot 1", "bot-b": "Bot 2", "bot-c": "Bot 3"}, false, true},
		{"cancel back", "open", "4\n6\n0\n0\nq\n", map[string]string{"bot-a": "Bot 1"}, true, true},
		{"cancel escape", "open", "4\n6\n\x1b0\nq\n", map[string]string{"bot-a": "Bot 1"}, true, true},
		{"closed", "closed", "q\n", map[string]string{"bot-a": "Bot 1"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, "\n\n3\n1\n"+tc.input)
			f.replies(t, cmd, map[string][]sessionReply{"lobby": {{Body: botLobbyView(tc.bots, false, tc.status)}}})
			runSession(t, cmd, out)
			assertCommands(t, f, "login", "lobbies", "lobby")
			if strings.Contains(out.String(), "5) Add bot (owner only)") != tc.add || strings.Contains(out.String(), "6) Remove bot (owner only)") != tc.remove {
				t.Fatal(out)
			}
		})
	}
}

func TestSessionBotMutationRecoversWithFetchOnly(t *testing.T) {
	for _, command := range []string{"lobby-add-bot", "lobby-remove-bot"} {
		for _, outcome := range []string{"committed", "unchanged", "closed", "missing response"} {
			t.Run(command+"/"+outcome, func(t *testing.T) {
				f := newSessionFixture(t, sessionConfig)
				beforeBots, afterBots := map[string]string{}, map[string]string{"bot-a": "Bot 1"}
				action := "5\n"
				if command == "lobby-remove-bot" {
					beforeBots, afterBots = afterBots, beforeBots
					action = "6\n1\n"
				}
				if outcome == "unchanged" {
					afterBots = beforeBots
				}
				status := "open"
				if outcome == "closed" {
					status = "closed"
				}
				reply := sessionReply{Failed: true}
				if outcome == "missing response" {
					reply = sessionReply{Body: `{"lobby":{"id":"lobby-one"}}`}
				}
				cmd, out := f.command(t, "\n\n3\n1\n4\n"+action+"r\nq\n")
				f.replies(t, cmd, map[string][]sessionReply{
					"lobby": {{Body: botLobbyView(beforeBots, true, "open")}, {Failed: true}, {Body: botLobbyView(afterBots, false, status)}},
					command: {reply},
				})
				runSession(t, cmd, out)
				calls := assertCommands(t, f, "login", "lobbies", "lobby", command, "lobby", "lobby")
				if !reflect.DeepEqual(calls[2], calls[4]) || !reflect.DeepEqual(calls[4], calls[5]) {
					t.Fatal("recovery changed lobby fetch arguments", calls)
				}
				assertText(t, out, "Lobby fetch failed", "Ready: 0/1 humans")
				if outcome != "missing response" {
					assertText(t, out, "outcome may be uncertain")
				}
				if outcome == "closed" {
					assertText(t, out, "1) Open match")
				}
			})
		}
	}
}

func TestSessionBotRecoveryFailureLeavesLobby(t *testing.T) {
	for _, unusable := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel fetch", true: "invalid fetch"}[unusable], func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			tail := "0\nq\n"
			recovery := sessionReply{Failed: true}
			if unusable {
				tail = "q\n"
				recovery = sessionReply{Body: `{"lobby":{"id":"lobby-one"}}`}
			}
			cmd, out := f.command(t, "\n\n3\n1\n4\n5\n"+tail)
			f.replies(t, cmd, map[string][]sessionReply{
				"lobby":         {{Body: botLobbyView(nil, true, "open")}, recovery},
				"lobby-add-bot": {{Failed: true}},
			})
			runSession(t, cmd, out)
			assertCommands(t, f, "login", "lobbies", "lobby", "lobby-add-bot", "lobby")
			if strings.Count(out.String(), "Lobby: Friday") != 1 {
				t.Fatal("stale lobby was redisplayed", out)
			}
		})
	}
}

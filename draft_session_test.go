package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const draftSessionStub = `#!/usr/bin/env bash
set -eu
jq -cn --args '$ARGS.positional' -- "$@" >> "$SESSION_LOG"
case "$1" in
 login) printf 'draft-token\n' ;;
 matches) printf '{"matches":[]}' ;;
 match)
  progress=$(jq -s '[.[] | select(.[0] == "draft" or .[0] == "draft-pick")] | length' "$SESSION_LOG")
  current='{"status":"setup","draft":{"step":"chantry"}}'
  if ((progress > 0)); then
   latest=$(jq -r --argjson n "$progress" '[.[:$n][] | select(.failed != true)] | last | .body // ""' "$DRAFT_REPLIES")
   [[ -n "$latest" ]] && current=$latest
  fi
  id=''
  for ((i=1;i<=$#;i++)); do
   if [[ "${!i}" == --id ]]; then ((i+=1)); id=${!i}; break; fi
  done
  jq -cn --arg id "$id" --argjson current "$current" '{match:{id:$id,name:"Draft match",status:$current.status,participants:["a","b"],wizards:{a:{draftcomplete:($current.draft.step == "complete")},b:{draftcomplete:false}},you:{draft:$current.draft}}}'
  ;;
 draft|draft-pick)
  index=$(jq -s '[.[] | select(.[0] == "draft" or .[0] == "draft-pick")] | length - 1' "$SESSION_LOG")
  if [[ $(jq -r --argjson i "$index" '.[$i].failed // false' "$DRAFT_REPLIES") == true ]]; then
   printf 'HTTP 409: stale_revision: refresh the draft\n' >&2
   exit 1
  fi
  jq -er --argjson i "$index" '.[$i].body // error("unexpected request")' "$DRAFT_REPLIES"
  ;;
 *) printf '{"action":"%s"}\n' "$1" ;;
esac
`

const draftUUIDStub = `#!/usr/bin/env bash
set -eu
n=1
if [[ -f "$DRAFT_UUID_LOG" ]]; then
 while IFS= read -r line; do ((n+=1)); done < "$DRAFT_UUID_LOG"
fi
printf '12345678-1234-5678-9abc-%012d\n' "$n" >> "$DRAFT_UUID_LOG"
printf '12345678-1234-5678-9abc-%012d\n' "$n"
`

type draftReply struct {
	Body   string `json:"body,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

func draftStep(step string, revision int) string {
	return fmt.Sprintf(`{"draft":{"step":%q},"cards":[{"id":%q,"name":%q,"placeholder":true,"income":{"fire":2},"affinity":{"fire":1},"cost":{"gold":3},"playrequirements":{"fire":1},"expertise":{"fire":1},"resources":{"gold":1},"element":"fire","victorypath":"fame","persistence":"persistent","disposal":"discard","effects":["starting_minion"]}],"revision":%d,"status":"setup"}`, step, "card "+step+" $(touch injected)", "Name "+step, revision)
}
func draftComplete(status string) string {
	return `{"draft":{"step":"complete"},"cards":[],"revision":6,"status":"` + status + `"}`
}

func draftSession(t *testing.T, input string, replies ...draftReply) (sessionFixture, *exec.Cmd, *bytes.Buffer) {
	t.Helper()
	f := newSessionFixture(t, sessionConfig)
	toolsDir := filepath.Dir(f.script)
	for name, script := range map[string]string{"grimcli": draftSessionStub, "uuidgen": draftUUIDStub} {
		if err := os.WriteFile(filepath.Join(toolsDir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(replies)
	if err != nil {
		t.Fatal(err)
	}
	replyPath := filepath.Join(f.dir, "replies.json")
	if err := os.WriteFile(replyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd, output := f.command(t, "\n\n"+input)
	cmd.Env = append(cmd.Env, "DRAFT_REPLIES="+replyPath, "DRAFT_UUID_LOG="+filepath.Join(f.dir, "uuids.log"), "PATH="+toolsDir+":"+os.Getenv("PATH"))
	return f, cmd, output
}

func draftArg(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "--"+flag {
			return args[i+1]
		}
	}
	t.Fatalf("missing --%s in %q", flag, args)
	return ""
}
func runDraftSession(t *testing.T, cmd *exec.Cmd, output *bytes.Buffer) {
	t.Helper()
	if err := cmd.Run(); err != nil {
		t.Fatalf("session: %v\n%s", err, output)
	}
	if strings.Contains(output.String(), "draft-token") || strings.Contains(output.String(), "config-password") {
		t.Fatalf("credentials exposed: %s", output)
	}
}

func TestSessionDraftThreeSteps(t *testing.T) {
	f, cmd, out := draftSession(t, "4\nm\nmatch one $(touch injected)\n1\n2\n-1\n1.0\n999999999999999999999999999\n01\n1\n1\n1\n3\n2\n0\n0\n0\nq\n",
		draftReply{Body: draftStep("chantry", 0)}, draftReply{Body: draftStep("victory_path", 1)}, draftReply{Body: draftStep("minion", 4)}, draftReply{Body: draftComplete("playing")})
	runDraftSession(t, cmd, out)
	calls := draftCalls(t, f)
	if len(calls) != 6 {
		t.Fatalf("calls: %q\n%s", calls, out)
	}
	for i, step := range []string{"chantry", "victory_path", "minion"} {
		args := calls[i+2]
		if args[0] != "draft-pick" || draftArg(t, args, "type") != "choose_"+step || draftArg(t, args, "cardid") != "card "+step+" $(touch injected)" || draftArg(t, args, "revision") != []string{"0", "1", "4"}[i] || draftArg(t, args, "requestid") != fmt.Sprintf("12345678-1234-5678-9abc-%012d", i+1) {
			t.Fatalf("pick: %q", args)
		}
	}
	for _, args := range calls[1:] {
		if draftArg(t, args, "id") != "match one $(touch injected)" {
			t.Fatal(args)
		}
	}
	for _, text := range []string{"1) Begin/resume draft", "Game started", "Picks are irreversible", "WARNING: Placeholder", "design ID: card chantry", "Name victory_path", "income:", "cost:", "playrequirements:", "affinity:", "expertise:", "resources:", "element:", "victorypath:", "persistence:", "disposal:", "effects:"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing %q in %s", text, out)
		}
	}
	if strings.Count(out.String(), "Choose one of the offered card numbers.") != 5 {
		t.Fatalf("invalid number handling: %s", out)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("shell argument evaluated")
	}
}

func TestSessionDraftResumeAndCompletion(t *testing.T) {
	for _, status := range []string{"setup", "playing"} {
		for _, alreadyComplete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-complete=%t", status, alreadyComplete), func(t *testing.T) {
				input := "4\nm\nselected match\n1\n"
				replies := []draftReply{}
				if !alreadyComplete {
					replies = append(replies, draftReply{Body: draftStep("minion", 5)})
					input += "1\n"
				}
				replies = append(replies, draftReply{Body: draftComplete(status)})
				f, cmd, out := draftSession(t, input+"0\n0\nq\n", replies...)
				runDraftSession(t, cmd, out)
				calls := draftCalls(t, f)
				if len(calls) != 2+map[bool]int{false: 1, true: 0}[alreadyComplete] || calls[1][0] != "draft" || draftArg(t, calls[1], "id") != "selected match" {
					t.Fatal(calls)
				}
				if alreadyComplete && strings.Contains(out.String(), "Choose an offered card") {
					t.Fatal("completed draft prompted for cards")
				}
				if !alreadyComplete && (draftArg(t, calls[2], "type") != "choose_minion" || draftArg(t, calls[2], "revision") != "5") {
					t.Fatal(calls)
				}
				want := "Draft complete; waiting for other players"
				if status == "playing" {
					want = "Game started"
				}
				if !strings.Contains(out.String(), want) {
					t.Fatalf("completion: %s", out)
				}
			})
		}
	}
}

func TestSessionDraftExactRetry(t *testing.T) {
	f, cmd, out := draftSession(t, "4\nm\nmatch\n1\n1\nx\nr\nr\n0\n",
		draftReply{Body: draftStep("minion", 5)}, draftReply{Failed: true}, draftReply{Failed: true}, draftReply{Body: draftComplete("setup")})
	runDraftSession(t, cmd, out)
	calls := draftCalls(t, f)
	if len(calls) != 5 || !reflect.DeepEqual(calls[2], calls[3]) || !reflect.DeepEqual(calls[3], calls[4]) {
		t.Fatalf("retry changed argv: %q", calls)
	}
	uuids, err := os.ReadFile(filepath.Join(f.dir, "uuids.log"))
	if err != nil || strings.Count(string(uuids), "\n") != 1 {
		t.Fatalf("generated another UUID on retry: %s %v", uuids, err)
	}
	if !strings.Contains(out.String(), "HTTP 409: stale_revision") || !strings.Contains(out.String(), "waiting for other players") {
		t.Fatal(out)
	}
}

func TestSessionDraftRefresh(t *testing.T) {
	f, cmd, out := draftSession(t, "4\nm\nmatch\n1\n1\nf\nr\n1\n0\n",
		draftReply{Body: draftStep("chantry", 0)}, draftReply{Failed: true}, draftReply{Failed: true}, draftReply{Body: draftStep("victory_path", 3)}, draftReply{Body: draftComplete("playing")})
	runDraftSession(t, cmd, out)
	calls := draftCalls(t, f)
	if len(calls) != 6 || calls[3][0] != "draft" || !reflect.DeepEqual(calls[3], calls[4]) || calls[5][0] != "draft-pick" {
		t.Fatalf("refresh: %q", calls)
	}
	if draftArg(t, calls[5], "type") != "choose_victory_path" || draftArg(t, calls[5], "revision") != "3" || draftArg(t, calls[2], "requestid") == draftArg(t, calls[5], "requestid") {
		t.Fatal(calls)
	}
}

func TestSessionDraftCancellationAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		replies     []draftReply
		commands    []string
	}{
		{"cancel ID", "4\nm\npartial\x1b0\nq\n", nil, []string{"login", "matches"}},
		{"cancel choice", "4\nm\nmatch\n1\n\x1b0\n0\nq\n", []draftReply{{Body: draftStep("chantry", 0)}}, []string{"login", "matches", "match", "draft", "match"}},
		{"back after GET failure", "4\nm\nmatch\n1\n0\n0\n0\nq\n", []draftReply{{Failed: true}}, []string{"login", "matches", "match", "draft", "match"}},
		{"cancel after POST failure", "4\nm\nmatch\n1\n1\n\x1b0\n0\nq\n", []draftReply{{Body: draftStep("minion", 5)}, {Failed: true}}, []string{"login", "matches", "match", "draft", "draft-pick", "match"}},
		{"reopen after failed pick", "4\nm\nmatch\n1\n1\nb\n1\n0\n0\nq\n", []draftReply{{Body: draftStep("minion", 5)}, {Failed: true}, {Body: draftComplete("playing")}}, []string{"login", "matches", "match", "draft", "draft-pick", "match", "draft", "match"}},
		{"retry initial GET", "4\nm\nmatch\n1\nr\n0\n0\nq\n", []draftReply{{Failed: true}, {Body: draftComplete("setup")}}, []string{"login", "matches", "match", "draft", "draft", "match"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, cmd, out := draftSession(t, tc.input, tc.replies...)
			runDraftSession(t, cmd, out)
			assertCommands(t, f, tc.commands...)
		})
	}
}

func TestSessionDraftUUIDDependency(t *testing.T) {
	for _, missing := range []bool{false, true} {
		f, cmd, out := draftSession(t, "4\nm\nmatch\n1\n1\n0\n", draftReply{Body: draftStep("chantry", 0)})
		toolsDir := filepath.Dir(f.script)
		// An isolated PATH makes the missing-dependency check deterministic.
		for _, name := range []string{"bash", "jq", "dirname"} {
			path, err := exec.LookPath(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path, filepath.Join(toolsDir, name)); err != nil {
				t.Fatal(err)
			}
		}
		if missing {
			cmd.Stdin = strings.NewReader("\n\n4\nm\nmatch\n1\n0\n0\nq\n")
			os.Remove(filepath.Join(toolsDir, "uuidgen"))
		} else {
			os.WriteFile(filepath.Join(toolsDir, "uuidgen"), []byte("#!/usr/bin/env bash\nexit 1\n"), 0700)
		}
		cmd.Env = append(cmd.Env, "PATH="+toolsDir)
		runDraftSession(t, cmd, out)
		want := "Could not generate a draft request UUID"
		callCount := 2
		if missing {
			want = "uuidgen is required for drafting"
			callCount = 1
		}
		if !strings.Contains(out.String(), want) || len(draftCalls(t, f)) != callCount {
			t.Fatalf("dependency handling: %s calls %q", out, f.calls(t))
		}
	}
}

func TestSessionDraftOfferedSecondCard(t *testing.T) {
	var response map[string]any
	if err := json.Unmarshal([]byte(draftStep("chantry", 0)), &response); err != nil {
		t.Fatal(err)
	}
	response["cards"] = append(response["cards"].([]any), map[string]any{"id": "second-card", "name": "Second Chantry", "placeholder": false, "income": map[string]int{"water": 1}})
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	f, cmd, out := draftSession(t, "4\nm\nmatch\n1\n2\n0\n", draftReply{Body: string(data)}, draftReply{Body: draftComplete("setup")})
	runDraftSession(t, cmd, out)
	calls := draftCalls(t, f)
	if len(calls) != 3 || draftArg(t, calls[2], "cardid") != "second-card" || !strings.Contains(out.String(), "2) Second Chantry [design ID: second-card]") || strings.Count(out.String(), "WARNING: Placeholder") != 1 {
		t.Fatalf("offered second card: %q\n%s", calls, out)
	}
}

// Keep the exact-payload assertions focused on draft and catalogue requests;
// navigation and authoritative match refreshes are asserted separately.
func draftCalls(t *testing.T, f sessionFixture) [][]string {
	t.Helper()
	var calls [][]string
	for _, args := range f.calls(t) {
		if args[0] != "match" && args[0] != "matches" {
			calls = append(calls, args)
		}
	}
	return calls
}

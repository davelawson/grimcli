package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Record exact argv boundaries. Scripted replies use each command's invocation
// count, allowing independent failures of a mutation and its follow-up GET.
const sessionStub = `#!/usr/bin/env bash
set -eu
jq -cn --args '$ARGS.positional' -- "$@" >> "$SESSION_LOG"
command_name=$1
shift
email=config-user
password=config-password
id=''
name=''
ready=true
while (($#)); do
 case "$1" in
  --email) email=$2 ;;
  --password) password=$2 ;;
  --id) id=$2 ;;
  --name) name=$2 ;;
  --ready=*) ready=${1#--ready=}; shift; continue ;;
 esac
 shift 2
done
if [[ "$command_name" == login ]]; then
 if [[ "$password" == rejected ]]; then printf 'HTTP 401: unauthorized\n' >&2; exit 1; fi
 printf 'token:%s\n' "$email"
 exit
fi
if [[ "$command_name" == "${SESSION_FAIL:-}" ]]; then printf 'HTTP 401: unauthorized\n' >&2; exit 1; fi
if [[ -n "${SESSION_REPLIES:-}" ]]; then
 index=$(jq -s --arg c "$command_name" '[.[] | select(.[0] == $c)] | length - 1' "$SESSION_LOG")
 if jq -e --arg c "$command_name" --argjson i "$index" '.[$c][$i] != null' "$SESSION_REPLIES" >/dev/null; then
  if [[ $(jq -r --arg c "$command_name" --argjson i "$index" '.[$c][$i].failed // false' "$SESSION_REPLIES") == true ]]; then
   printf 'HTTP 409: scripted failure\n' >&2; exit 1
  fi
  jq -r --arg c "$command_name" --argjson i "$index" '.[$c][$i].body // ""' "$SESSION_REPLIES"
  exit
 fi
fi
lobby() {
 jq -cn --arg id "$id" --argjson ready "$ready" '{lobby:{id:$id,name:"Friday",owner:"owner",status:"open",members:["owner","user $(touch injected) ; *"],readiness:{owner:true,"user $(touch injected) ; *":$ready},matchid:null}}'
}
match() {
 jq -cn --arg id "$id" '{match:{id:$id,name:"Friday match",status:"setup",participants:["owner","other"],wizards:{owner:{draftcomplete:false},other:{draftcomplete:true}},you:{draft:{step:"chantry"}},turn:null,outcome:null}}'
}
case "$command_name" in
 public-lobbies) printf '{"lobbies":[{"id":"public-one","name":"Public Friday","membercount":1}]}' ;;
 lobbies) printf '{"lobbies":[{"id":"lobby-one","status":"open"}]}' ;;
 matches) printf '{"matches":[{"id":"match-one","status":"setup"}]}' ;;
 lobby-create) printf '{"id":"created-lobby"}' ;;
 lobby-join|lobby-ready|lobby) lobby ;;
 lobby-launch) id=launched-match; match ;;
 lobby-leave|lobby-remove-user|lobby-delete) ;;
 catalogue) printf '{"catalogue":{"cards":[]}}' ;;
 match) match ;;
 *) printf 'Unexpected command\n' >&2; exit 1 ;;
esac
`

type sessionFixture struct{ dir, script, log string }

func newSessionFixture(t *testing.T, configText string) sessionFixture {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("session tests require %s: %v", tool, err)
		}
	}
	dir := t.TempDir()
	binDir := filepath.Join(dir, "session tools")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("grimcli-session.sh")
	if err != nil {
		t.Fatal(err)
	}
	f := sessionFixture{dir: dir, script: filepath.Join(binDir, "grimcli-session.sh"), log: filepath.Join(dir, "calls.jsonl")}
	for path, contents := range map[string][]byte{f.script: source, filepath.Join(binDir, "grimcli"): []byte(sessionStub)} {
		if err := os.WriteFile(path, contents, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if configText != "" {
		if err := os.WriteFile(filepath.Join(dir, "grimcli.json"), []byte(configText), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f sessionFixture) command(t *testing.T, input string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "bash", append([]string{f.script}, args...)...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "SESSION_LOG="+f.log)
	cmd.Stdin = strings.NewReader(input)
	output := new(bytes.Buffer)
	cmd.Stdout, cmd.Stderr = output, output
	return cmd, output
}

func (f sessionFixture) calls(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var args []string
		if err := json.Unmarshal(line, &args); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, args)
	}
	return calls
}

type sessionReply struct {
	Body   string `json:"body,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

func (f sessionFixture) replies(t *testing.T, cmd *exec.Cmd, replies map[string][]sessionReply) {
	t.Helper()
	data, err := json.Marshal(replies)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.dir, "replies.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(cmd.Env, "SESSION_REPLIES="+path)
}
func runSession(t *testing.T, cmd *exec.Cmd, out *bytes.Buffer) {
	t.Helper()
	if err := cmd.Run(); err != nil {
		t.Fatalf("session: %v\n%s", err, out)
	}
	for _, secret := range []string{"token:config-user", "config-password", "token:alice", "token:bob", "new-password", "rejected"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("output exposes %q: %s", secret, out)
		}
	}
}
func assertCommands(t *testing.T, f sessionFixture, want ...string) [][]string {
	t.Helper()
	calls := f.calls(t)
	var got []string
	for _, args := range calls {
		got = append(got, args[0])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands %q, want %q; calls %q", got, want, calls)
	}
	return calls
}
func assertText(t *testing.T, out *bytes.Buffer, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if !strings.Contains(out.String(), text) {
			t.Errorf("missing %q in %s", text, out)
		}
	}
}

const sessionConfig = `{"server":"https://localhost:8080","cacert":"$GRIM_SSL/grim.crt","email":"config-user","password":"config-password"}`

func TestSessionCreateReadyLaunch(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	name := "Friday $(touch injected) `touch injected` *"
	cmd, out := f.command(t, "\n\n1\n"+name+"\n2\n3\n1\n5\n3\n2\n0\n0\nq\n")
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "lobby-create", "lobby", "lobby-ready", "lobby", "lobby-ready", "lobby-launch", "catalogue")
	if draftArg(t, calls[1], "name") != name || draftArg(t, calls[2], "id") != "created-lobby" || calls[3][3] != "--ready=false" || calls[5][3] != "--ready=true" || draftArg(t, calls[6], "id") != "created-lobby" {
		t.Fatal(calls)
	}
	assertText(t, out, "Home", "Ready: 1/2", "Waiting for every player", "5) Launch match (owner only)", "Match launched", "Players: 2", "Drafts complete: 1/2", "1) Begin/resume draft", `"catalogue"`)
	if _, err := os.Stat(filepath.Join(f.dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("shell argument evaluated")
	}
}

func TestSessionJoinNumberedManualAndRemembered(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n2\n01\n999999999999999999999\n1\n0\n0\n3\ns\n0\nm\nmanual lobby\n0\nr\n0\n4\n1\n0\ns\n0\n0\nq\n")
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "public-lobbies", "lobby-join", "lobbies", "lobby", "lobby", "lobbies", "matches", "match", "match")
	for i, id := range map[int]string{2: "public-one", 4: "public-one", 5: "manual lobby", 8: "match-one", 9: "match-one"} {
		if draftArg(t, calls[i], "id") != id {
			t.Fatal(calls)
		}
	}
	assertText(t, out, "Public Friday — 1/4 players [ID: public-one]", "Reopen remembered selection [ID: public-one]", "Reopen remembered selection [ID: match-one]", "Choose one of the listed numbers or actions")
}

func TestSessionEmptyLists(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n2\nr\nm\nmanual\n0\n0\n3\n0\n4\n0\nq\n")
	f.replies(t, cmd, map[string][]sessionReply{"public-lobbies": {{Body: `{"lobbies":[]}`}, {Body: `{"lobbies":[]}`}}, "lobbies": {{Body: `{"lobbies":[]}`}}, "matches": {{Body: `{"matches":[]}`}}})
	runSession(t, cmd, out)
	assertCommands(t, f, "login", "public-lobbies", "public-lobbies", "lobby-join", "lobbies", "matches")
	assertText(t, out, "No public lobbies", "No lobby memberships", "No matches", "Joined lobby")
}

func TestSessionRosterRemovalLeaveAndDelete(t *testing.T) {
	for _, action := range []string{"1", "3"} {
		t.Run(action, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, "\n\n3\n1\n4\n2\nx\n01\n2\n4\n0\n4\n"+action+"\nq\n")
			runSession(t, cmd, out)
			last := map[string]string{"1": "lobby-leave", "3": "lobby-delete"}[action]
			calls := assertCommands(t, f, "login", "lobbies", "lobby", "lobby-remove-user", "lobby", last)
			if draftArg(t, calls[3], "userid") != "user $(touch injected) ; *" {
				t.Fatal(calls)
			}
			assertText(t, out, "Remove member (owner only)", "Choose one of the listed member numbers", `"readiness"`)
			if strings.Count(out.String(), "Home\n") != 2 {
				t.Fatalf("leave/delete did not return Home: %s", out)
			}
		})
	}
}

func TestSessionFetchAndMutationRetries(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n1\nFriday\nx\nr\n4\n2\n1\nr\nr\n0\n0\nq\n")
	f.replies(t, cmd, map[string][]sessionReply{"lobby": {{Failed: true}, {Body: lobbyView("created-lobby", "open", "")}, {Failed: true}, {Body: lobbyView("created-lobby", "open", "")}}, "lobby-remove-user": {{Failed: true}, {Body: ""}}})
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "lobby-create", "lobby", "lobby", "lobby-remove-user", "lobby-remove-user", "lobby", "lobby")
	if !reflect.DeepEqual(calls[2], calls[3]) || !reflect.DeepEqual(calls[4], calls[5]) || !reflect.DeepEqual(calls[6], calls[7]) {
		t.Fatal("retry changed args", calls)
	}
	assertText(t, out, "HTTP 409", "Lobby fetch failed", "Remove member failed")
}

func TestSessionLaunchRetryAndFollowupFetch(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n3\n1\n5\nr\nr\n0\n0\nq\n")
	f.replies(t, cmd, map[string][]sessionReply{"lobby-launch": {{Failed: true}, {Body: `{"match":{"id":"launched-match"}}`}}, "match": {{Failed: true}, {Body: matchView("launched-match", "setup", "chantry")}}})
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "lobbies", "lobby", "lobby-launch", "lobby-launch", "match", "match")
	if !reflect.DeepEqual(calls[3], calls[4]) || !reflect.DeepEqual(calls[5], calls[6]) {
		t.Fatal(calls)
	}
	assertText(t, out, "Match launched", "1) Begin/resume draft")
}

func TestSessionFailuresKeepSelections(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n3\n1\n1\n0\n4\n1\n0\n0\n0\ns\n0\nm\nmissing\n0\ns\n0\n0\nq\n")
	f.replies(t, cmd, map[string][]sessionReply{"lobby-ready": {{Failed: true}}, "lobby-leave": {{Failed: true}}, "lobby": {{Body: lobbyView("lobby-one", "open", "")}, {Body: lobbyView("lobby-one", "open", "")}, {Failed: true}}})
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "lobbies", "lobby", "lobby-ready", "lobby-leave", "lobby", "lobby", "lobby")
	if draftArg(t, calls[7], "id") != "lobby-one" {
		t.Fatal("failed manual selection replaced remembered ID", calls)
	}
	assertText(t, out, "Reopen remembered selection [ID: lobby-one]")
}

func lobbyView(id, status, matchID string) string {
	data, _ := json.Marshal(map[string]any{"lobby": map[string]any{"id": id, "name": "Friday", "owner": "owner", "status": status, "members": []string{"owner"}, "readiness": map[string]bool{"owner": false}, "matchid": matchID}})
	return string(data)
}
func matchView(id, status, step string) string {
	return `{"match":{"id":"` + id + `","name":"Friday match","status":"` + status + `","participants":["owner","other"],"wizards":{"owner":{"draftcomplete":true},"other":{"draftcomplete":false}},"you":{"draft":{"step":"` + step + `"}},"turn":{"number":2,"activewizard":"owner","phase":"action","step":"cast"},"outcome":"owner wins"}}`
}

func TestSessionClosedLobbyAndMatchStates(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "\n\n3\n1\n3\n1\n2\n2\n3\n1\n0\n0\n0\nq\n")
	f.replies(t, cmd, map[string][]sessionReply{"lobby": {{Body: lobbyView("lobby-one", "closed", "match-one")}}, "match": {{Body: matchView("match-one", "setup", "complete")}, {Body: matchView("match-one", "playing", "complete")}, {Body: matchView("match-one", "finished", "complete")}}})
	runSession(t, cmd, out)
	assertCommands(t, f, "login", "lobbies", "lobby", "match", "match", "match")
	assertText(t, out, "1) Open match", "Draft complete; waiting for other players", "Game started", "Active wizard: owner", "Turn: 2", "Phase: action", "Step: cast", "Outcome: owner wins")
	for _, text := range []string{"Begin/resume draft", "Mark me ready", "Launch match", "Leave\n"} {
		if strings.Contains(out.String(), text) {
			t.Errorf("unexpected action %q", text)
		}
	}
}

func TestSessionOtherLobbyStates(t *testing.T) {
	for _, status := range []string{"closed", "deleted", "unknown"} {
		t.Run(status, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, "\n\n3\n1\n1\n2\n0\n0\nq\n")
			f.replies(t, cmd, map[string][]sessionReply{"lobby": {{Body: lobbyView("lobby-one", status, "")}, {Body: lobbyView("lobby-one", status, "")}}})
			runSession(t, cmd, out)
			assertCommands(t, f, "login", "lobbies", "lobby", "lobby")
			if strings.Contains(out.String(), "1) Open match") || strings.Contains(out.String(), "Mark me ready") {
				t.Fatal(out)
			}
		})
	}
}

func TestSessionLoginRetryAndAccountChange(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "alice\nrejected\nr\nalice\n\n3\n1\n0\n0\n4\n1\n0\n0\n5\n1\n\nnew-password\n0\n3\n0\n4\n0\nq\n")
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "login", "lobbies", "lobby", "matches", "match", "login", "lobbies", "matches")
	if draftArg(t, calls[0], "password") != "rejected" || draftArg(t, calls[1], "email") != "alice" || draftArg(t, calls[6], "password") != "new-password" || draftArg(t, calls[7], "token") != "token:config-user" {
		t.Fatal(calls)
	}
	// Both remembered selections must disappear after successful account change.
	after := strings.Split(out.String(), "Logged in.")
	if strings.Contains(after[len(after)-1], "Reopen remembered selection") {
		t.Fatal("account switch retained a selection", out)
	}
}

func TestSessionCancelledAndFailedLoginPreservesSession(t *testing.T) {
	for _, attempt := range []string{"other\x1b", "other\npartial-secret\x1b", "other\nrejected\n0\n", "other\nrejected\n\x1b"} {
		t.Run(attempt, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, "\n\n3\n1\n0\n0\n4\n1\n0\n0\n5\n1\n"+attempt+"0\n3\ns\n0\n0\n4\ns\n0\n0\nq\n")
			runSession(t, cmd, out)
			calls := f.calls(t)
			for _, args := range calls {
				if args[0] != "login" && draftArg(t, args, "token") != "token:config-user" {
					t.Fatal(calls)
				}
			}
			assertText(t, out, "Reopen remembered selection [ID: lobby-one]", "Reopen remembered selection [ID: match-one]")
			if strings.Contains(out.String(), "partial-secret") {
				t.Fatal(out)
			}
		})
	}
}

func TestSessionMissingConfigAndLiteralArguments(t *testing.T) {
	f := newSessionFixture(t, "")
	email := "alice $(touch injected) `touch injected`"
	password := "password with spaces; $(touch injected)"
	id := "match $(touch injected) *"
	cmd, out := f.command(t, "\nhttp://localhost:8080\n\n"+email+"\n\n"+password+"\n4\nm\n\n"+id+"\n3\n2\n")
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	calls := assertCommands(t, f, "login", "matches", "match", "catalogue")
	if !reflect.DeepEqual(calls[0], []string{"login", "--server", "http://localhost:8080", "--email", email, "--password", password}) || draftArg(t, calls[2], "id") != id || draftArg(t, calls[3], "id") != id {
		t.Fatal(calls)
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("session wrote files: %v", entries)
	}
	if strings.Contains(out.String(), password) || strings.Contains(out.String(), "token:"+email) {
		t.Fatal("credentials exposed", out)
	}
}

func TestSessionConcurrentInstances(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	other := f
	other.log = filepath.Join(f.dir, "other-calls.jsonl")
	first, out1 := f.command(t, "alice\nfirst-password\n4\nm\nalice-match\nq\n")
	second, out2 := other.command(t, "bob\nsecond-password\n4\nm\nbob-match\nq\n")
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range []*exec.Cmd{first, second} {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("session %d: %v %s %s", i, err, out1, out2)
		}
	}
	for i, fixture := range []sessionFixture{f, other} {
		user := []string{"alice", "bob"}[i]
		calls := assertCommands(t, fixture, "login", "matches", "match")
		if draftArg(t, calls[2], "token") != "token:"+user || draftArg(t, calls[2], "id") != user+"-match" {
			t.Fatal(calls)
		}
	}
}

func TestSessionEscapeCancelsInput(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		commands    []string
	}{
		{"home", "\n\n3\x1b3\n0\nq\n", []string{"login", "lobbies"}},
		{"list", "\n\n3\n\x1bq\n", []string{"login", "lobbies"}},
		{"manual ID", "\n\n3\nm\npartial\x1b0\nq\n", []string{"login", "lobbies"}},
		{"create name", "\n\n1\npartial\x1bq\n", []string{"login"}},
		{"roster", "\n\n3\n1\n4\n2\npartial\x1b0\n0\n0\nq\n", []string{"login", "lobbies", "lobby"}},
		{"lobby", "\n\n3\n1\n\x1b0\nq\n", []string{"login", "lobbies", "lobby"}},
		{"match", "\n\n4\n1\n\x1b0\nq\n", []string{"login", "matches", "match"}},
		{"initial email", "partial\x1b", nil},
		{"initial password", "\npartial-secret\x1b", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, tc.input)
			runSession(t, cmd, out)
			assertCommands(t, f, tc.commands...)
			assertText(t, out, "Cancelled.")
		})
	}
	f := newSessionFixture(t, "")
	cmd, out := f.command(t, "http://partial\x1b")
	runSession(t, cmd, out)
	assertCommands(t, f)
}

func TestSessionInputEditing(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, out := f.command(t, "wrong\x15alice\nsecreX\x7ft\n4\nm\nwrong\x15matchX\b1\nq\n")
	runSession(t, cmd, out)
	calls := assertCommands(t, f, "login", "matches", "match")
	if draftArg(t, calls[0], "email") != "alice" || draftArg(t, calls[0], "password") != "secret" || draftArg(t, calls[2], "id") != "match1" {
		t.Fatal(calls)
	}
}

func TestSessionControlKeysExit(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int
	}{{"interrupt password", "\npartial-secret\x03", 130}, {"eof password", "\npartial-secret\x04", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, tc.input)
			_ = cmd.Run()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.code {
				t.Fatalf("status %v: %s", cmd.ProcessState, out)
			}
			if len(f.calls(t)) != 0 || strings.Contains(out.String(), "partial-secret") {
				t.Fatal(out)
			}
		})
	}
}

func TestSessionConfigValidationAndHelp(t *testing.T) {
	for _, configText := range []string{"null", "[]", `{"password":123}`, `{} {}`, "malformed"} {
		t.Run(configText, func(t *testing.T) {
			f := newSessionFixture(t, configText)
			cmd, _ := f.command(t, "")
			if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 2 {
				t.Fatalf("invalid config must exit 2: %v", err)
			}
			if calls := f.calls(t); len(calls) != 0 {
				t.Fatalf("invalid config invoked CLI: %q", calls)
			}
		})
	}
	f := newSessionFixture(t, "")
	for _, args := range [][]string{{"--config", "missing.json"}, {"--config"}, {"--unknown"}} {
		cmd, _ := f.command(t, "", args...)
		if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 2 {
			t.Fatalf("invalid arguments %q must exit 2: %v", args, err)
		}
	}
	cmd, output := f.command(t, "", "--help")
	if err := cmd.Run(); err != nil || !strings.Contains(output.String(), "Usage:") {
		t.Fatalf("help: %v %s", err, output)
	}
	// An explicit config path with spaces is passed through unchanged.
	configPath := filepath.Join(f.dir, "other defaults.json")
	if err := os.WriteFile(configPath, []byte(sessionConfig), 0600); err != nil {
		t.Fatal(err)
	}
	cmd, output = f.command(t, "\n\nq\n", "--config", configPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("explicit config: %v %s", err, output)
	}
	if calls := f.calls(t); len(calls) != 1 || calls[0][2] != configPath {
		t.Fatalf("config path changed: %q", calls)
	}
}

func TestSessionFullLobbyDraftJourneys(t *testing.T) {
	for _, entry := range []string{"create", "join"} {
		t.Run(entry, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			toolsDir := filepath.Dir(f.script)
			if err := os.WriteFile(filepath.Join(toolsDir, "uuidgen"), []byte(draftUUIDStub), 0700); err != nil {
				t.Fatal(err)
			}
			start := "1\nFriday\n"
			commands := []string{"login", "lobby-create", "lobby"}
			if entry == "join" {
				start = "2\n1\n"
				commands = []string{"login", "public-lobbies", "lobby-join"}
			}
			cmd, out := f.command(t, "\n\n"+start+"1\n5\n1\n1\n1\n1\n2\n0\n"+map[string]string{"create": "q\n", "join": "0\nq\n"}[entry])
			cmd.Env = append(cmd.Env, "DRAFT_UUID_LOG="+filepath.Join(f.dir, "uuids.log"), "PATH="+toolsDir+":"+os.Getenv("PATH"))
			f.replies(t, cmd, map[string][]sessionReply{
				"draft":      {{Body: draftStep("chantry", 0)}},
				"draft-pick": {{Body: draftStep("victory_path", 1)}, {Body: draftStep("minion", 2)}, {Body: draftComplete("setup")}},
				"match":      {{Body: matchView("launched-match", "setup", "complete")}, {Body: matchView("launched-match", "playing", "complete")}},
			})
			runSession(t, cmd, out)
			commands = append(commands, "lobby-ready", "lobby-launch", "draft", "draft-pick", "draft-pick", "draft-pick", "match", "match")
			calls := assertCommands(t, f, commands...)
			launch := strings.Index(out.String(), "Match launched.")
			firstDraft := strings.Index(out.String(), "Draft step:")
			if launch < 0 || firstDraft <= launch || !strings.Contains(out.String()[launch:firstDraft], "Choose an action:") {
				t.Fatal("draft was not explicit", out)
			}
			if calls[len(calls)-2][0] != "match" {
				t.Fatal("no authoritative refresh after draft", calls)
			}
			assertText(t, out, "Picks are irreversible", "Draft complete; waiting for other players", "Game started", "Active wizard: owner")
		})
	}
}

func TestSessionReadinessAndLaunchAvailability(t *testing.T) {
	for _, tc := range []struct {
		name             string
		members          int
		ready, canLaunch bool
	}{
		{"needs players", 1, true, false}, {"needs readiness", 2, false, false},
		{"two ready", 2, true, true}, {"four ready", 4, true, true}, {"too many", 5, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			members := []string{}
			readiness := map[string]bool{}
			for i := 0; i < tc.members; i++ {
				id := string(rune('a' + i))
				members = append(members, id)
				readiness[id] = tc.ready
			}
			data, _ := json.Marshal(map[string]any{"lobby": map[string]any{"id": "lobby-one", "name": "Friday", "owner": "a", "status": "open", "members": members, "readiness": readiness}})
			cmd, out := f.command(t, "\n\n3\n1\n0\n0\nq\n")
			f.replies(t, cmd, map[string][]sessionReply{"lobby": {{Body: string(data)}}})
			runSession(t, cmd, out)
			if strings.Contains(out.String(), "5) Launch match (owner only)") != tc.canLaunch {
				t.Fatal(out)
			}
			assertText(t, out, "1) Mark me ready", "2) Mark me unready")
		})
	}
}

func TestSessionCreateJoinDeleteFailures(t *testing.T) {
	for _, tc := range []struct {
		command, input string
		commands       []string
	}{
		{"lobby-create", "1\nname\n0\n3\ns\n0\n0\nq\n", []string{"lobby-create", "lobbies", "lobby"}},
		{"lobby-join", "2\nm\nother\n0\n0\n3\ns\n0\n0\nq\n", []string{"public-lobbies", "lobby-join", "lobbies", "lobby"}},
		{"lobby-delete", "3\ns\n4\n3\n0\n0\n0\ns\n0\n0\nq\n", []string{"lobbies", "lobby", "lobby-delete", "lobby"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, out := f.command(t, "\n\n3\n1\n0\n0\n"+tc.input)
			cmd.Env = append(cmd.Env, "SESSION_FAIL="+tc.command)
			runSession(t, cmd, out)
			calls := assertCommands(t, f, append([]string{"login", "lobbies", "lobby"}, tc.commands...)...)
			if draftArg(t, calls[len(calls)-1], "id") != "lobby-one" {
				t.Fatal(calls)
			}
			assertText(t, out, "HTTP 401: unauthorized", "Reopen remembered selection [ID: lobby-one]")
		})
	}
}

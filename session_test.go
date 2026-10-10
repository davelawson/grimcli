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

// The stub records exact argv boundaries and returns the same output shapes as
// the CLI. All credentials and logs here are synthetic and live in t.TempDir.
const sessionStub = `#!/usr/bin/env bash
set -eu
jq -cn --args '$ARGS.positional' -- "$@" >> "$SESSION_LOG"
command_name=$1
shift
email=config-user
password=config-password
while (($#)); do
    case "$1" in
        --email) email=$2 ;;
        --password) password=$2 ;;
    esac
    shift 2
done
if [[ "$command_name" == login ]]; then
    if [[ "$password" == rejected ]]; then
        printf 'HTTP 401: unauthorized\n' >&2
        exit 1
    fi
    printf 'token:%s\n' "$email"
elif [[ "$command_name" == "${SESSION_FAIL:-}" ]]; then
    printf 'HTTP 401: unauthorized\n' >&2
    exit 1
else
    printf '{"action":"%s"}\n' "$command_name"
fi
`

type sessionFixture struct {
	dir, script, log string
}

func newSessionFixture(t *testing.T, configText string) sessionFixture {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("session tests require %s: %v", tool, err)
		}
	}
	dir := t.TempDir()
	// Include spaces in both the script and binary path.
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "bash", append([]string{f.script}, args...)...)
	cmd.Dir = f.dir
	cmd.Env = append(os.Environ(), "SESSION_LOG="+f.log, "SESSION_FAIL=matches")
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

const sessionConfig = `{"server":"https://localhost:8080","cacert":"$GRIM_SSL/grim.crt","email":"config-user","password":"config-password"}`

func TestSessionReusesValuesAndSurvivesFailure(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	input := "\n\n1\n2\n3\nmatch one\n4\n5\nmatch two\n3\n0\n"
	cmd, output := f.command(t, input)
	if err := cmd.Run(); err != nil {
		t.Fatalf("session: %v\n%s", err, output)
	}
	common := []string{"--config", "grimcli.json", "--server", "https://localhost:8080"}
	var want [][]string
	for _, item := range []struct{ command, id string }{{"login", ""}, {"matches", ""}, {"lobbies", ""}, {"match", "match one"}, {"catalogue", "match one"}, {"match", "match two"}} {
		args := append([]string{item.command}, common...)
		if item.command != "login" {
			args = append(args, "--token", "token:config-user")
		}
		if item.id != "" {
			args = append(args, "--id", item.id)
		}
		want = append(want, args)
	}
	if got := f.calls(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls:\n%q\nwant:\n%q", got, want)
	}
	if !strings.Contains(output.String(), "HTTP 401: unauthorized") || !strings.Contains(output.String(), `{"action":"catalogue"}`) {
		t.Fatalf("missing error or response: %s", output)
	}
	for _, secret := range []string{"token:config-user", "config-password"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("output exposes %q", secret)
		}
	}
}

func TestSessionLoginRetryAndAccountChange(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	// Each blank credential independently falls back to config. A successful
	// second login must clear the previous user's selected match.
	cmd, output := f.command(t, "alice\nrejected\nr\nalice\n\n3\nold-match\n6\n\nnew-password\n4\nnew-match\n0\n")
	if err := cmd.Run(); err != nil {
		t.Fatalf("session: %v\n%s", err, output)
	}
	calls := f.calls(t)
	if len(calls) != 5 {
		t.Fatalf("unexpected calls: %q", calls)
	}
	for _, tc := range []struct {
		index int
		tail  []string
	}{
		{0, []string{"--email", "alice", "--password", "rejected"}},
		{1, []string{"--email", "alice"}},
		{2, []string{"--token", "token:alice", "--id", "old-match"}},
		{3, []string{"--password", "new-password"}},
		{4, []string{"--token", "token:config-user", "--id", "new-match"}},
	} {
		got := calls[tc.index]
		if !reflect.DeepEqual(got[len(got)-len(tc.tail):], tc.tail) {
			t.Errorf("call %d: %q", tc.index, got)
		}
	}
	for _, secret := range []string{"token:alice", "token:config-user", "new-password", "rejected"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("output exposes %q", secret)
		}
	}
}

func TestSessionMissingConfigAndLiteralArguments(t *testing.T) {
	f := newSessionFixture(t, "")
	email := "alice $(touch injected) `touch injected`"
	password := "password with spaces; $(touch injected)"
	id := "match $(touch injected) *"
	// Blank required fields reprompt. EOF after the catalogue exits normally.
	cmd, output := f.command(t, "\nhttp://localhost:8080\n\n"+email+"\n\n"+password+"\n4\n\n"+id+"\n")
	if err := cmd.Run(); err != nil {
		t.Fatalf("session: %v\n%s", err, output)
	}
	want := [][]string{
		{"login", "--server", "http://localhost:8080", "--email", email, "--password", password},
		{"catalogue", "--server", "http://localhost:8080", "--token", "token:" + email, "--id", id},
	}
	if got := f.calls(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("literal argv: %q", got)
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 { // Only the test's binary directory and stub call log.
		t.Fatalf("session wrote unexpected files: %v", entries)
	}
	if strings.Contains(output.String(), password) || strings.Contains(output.String(), "token:"+email) {
		t.Fatalf("output exposes credentials: %s", output)
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
	cmd, output = f.command(t, "\n\n0\n", "--config", configPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("explicit config: %v %s", err, output)
	}
	if calls := f.calls(t); len(calls) != 1 || calls[0][2] != configPath {
		t.Fatalf("config path changed: %q", calls)
	}
}

func TestSessionConcurrentInstances(t *testing.T) {
	// Start both before waiting, using the same config and working directory.
	f := newSessionFixture(t, sessionConfig)
	other := f
	other.log = filepath.Join(f.dir, "other-calls.jsonl")
	first, firstOutput := f.command(t, "alice\nfirst-password\n3\nalice-match\n0\n")
	second, secondOutput := other.command(t, "bob\nsecond-password\n4\nbob-match\n0\n")
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range []*exec.Cmd{first, second} {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("session %d: %v\n%s\n%s", i, err, firstOutput, secondOutput)
		}
	}
	for i, fixture := range []sessionFixture{f, other} {
		user := []string{"alice", "bob"}[i]
		calls := fixture.calls(t)
		wantTail := []string{"--token", "token:" + user, "--id", user + "-match"}
		if len(calls) != 2 || !reflect.DeepEqual(calls[1][len(calls[1])-4:], wantTail) {
			t.Errorf("session %d mixed state: %q", i, calls)
		}
	}
}

func TestSessionEscapeCancelsInput(t *testing.T) {
	for _, tc := range []struct {
		name, input, lastCommand, id string
		callCount                    int
	}{
		{"menu", "\n\n3\x1b2\n0\n", "lobbies", "", 2},
		{"first match", "\n\n3\npartial-id\x1b2\n0\n", "lobbies", "", 2},
		{"change match", "\n\n5\nexisting\n5\nreplacement\x1b4\n0\n", "catalogue", "existing", 2},
		{"login email", "\n\n5\nexisting\n6\nother-user\x1b4\n0\n", "catalogue", "existing", 2},
		{"login password", "\n\n5\nexisting\n6\nother-user\npartial-secret\x1b4\n0\n", "catalogue", "existing", 2},
		{"login retry", "\n\n6\nother-user\nrejected\npartial-retry\x1b2\n0\n", "lobbies", "", 3},
		{"initial email", "partial-email\x1b", "", "", 0},
		{"initial password", "\npartial-secret\x1b", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, output := f.command(t, tc.input)
			if err := cmd.Run(); err != nil {
				t.Fatalf("cancel: %v\n%s", err, output)
			}
			calls := f.calls(t)
			if len(calls) != tc.callCount {
				t.Fatalf("cancelled action invoked CLI: %q", calls)
			}
			if tc.lastCommand != "" {
				last := calls[len(calls)-1]
				want := []string{tc.lastCommand, "--config", "grimcli.json", "--server", "https://localhost:8080", "--token", "token:config-user"}
				if tc.id != "" {
					want = append(want, "--id", tc.id)
				}
				if !reflect.DeepEqual(last, want) {
					t.Fatalf("cancel changed session: %q; want %q", last, want)
				}
			}
			if !strings.Contains(output.String(), "Cancelled.") || strings.Contains(output.String(), "partial-secret") {
				t.Fatalf("unexpected cancellation output: %s", output)
			}
		})
	}
	t.Run("initial server", func(t *testing.T) {
		f := newSessionFixture(t, "")
		cmd, output := f.command(t, "http://partial\x1b")
		if err := cmd.Run(); err != nil || len(f.calls(t)) != 0 {
			t.Fatalf("cancel startup: %v\n%s", err, output)
		}
	})
}

func TestSessionInputEditing(t *testing.T) {
	f := newSessionFixture(t, sessionConfig)
	cmd, output := f.command(t, "wrong\x15alice\nsecreX\x7ft\n5\nwrong\x15matchX\b1\n4\n0\n")
	if err := cmd.Run(); err != nil {
		t.Fatalf("editing: %v\n%s", err, output)
	}
	calls := f.calls(t)
	if len(calls) != 2 {
		t.Fatalf("unexpected calls: %q", calls)
	}
	if !reflect.DeepEqual(calls[0][len(calls[0])-4:], []string{"--email", "alice", "--password", "secret"}) ||
		!reflect.DeepEqual(calls[1][len(calls[1])-4:], []string{"--token", "token:alice", "--id", "match1"}) {
		t.Fatalf("editing produced wrong arguments: %q", calls)
	}
}

func TestSessionControlKeysExit(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int
	}{
		{"interrupt password", "\npartial-secret\x03", 130},
		{"eof password", "\npartial-secret\x04", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSessionFixture(t, sessionConfig)
			cmd, output := f.command(t, tc.input)
			_ = cmd.Run()
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.code {
				t.Fatalf("wrong exit status: %v\n%s", cmd.ProcessState, output)
			}
			if len(f.calls(t)) != 0 || strings.Contains(output.String(), "partial-secret") {
				t.Fatalf("interrupted password was used or exposed: %s", output)
			}
		})
	}
}

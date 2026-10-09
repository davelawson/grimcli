package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davelawson/grim/api"
)

func isolatedDirectory(t *testing.T) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Error(err)
		}
	})
}

func writeConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigDefaultsAndOverrides(t *testing.T) {
	isolatedDirectory(t)
	t.Setenv("GRIM_SSL", "/certificates")
	writeConfig(t, "grimcli.json", `{"server":"https://localhost:8080","cacert":"${GRIM_SSL}/grim.crt","email":"bob@aol.com","password":"password123"}`)
	o, _, err := parse([]string{"login"}, &bytes.Buffer{})
	if err != nil || o.server != "https://localhost:8080" || o.caCert != "/certificates/grim.crt" || o.email != "bob@aol.com" || o.password != "password123" {
		t.Fatalf("defaults were not applied: %v", err)
	}
	o, _, err = parse([]string{"login", "--server", "http://example.com/", "--cacert=", "--email", "alice", "--password", "other"}, &bytes.Buffer{})
	if err != nil || o.server != "http://example.com" || o.caCert != "" || o.email != "alice" || o.password != "other" {
		t.Fatalf("overrides were not applied: %v", err)
	}
	for _, flag := range []string{"--email=", "--password=", "--server="} {
		if _, _, err := parse([]string{"login", flag}, &bytes.Buffer{}); err == nil {
			t.Fatalf("explicit empty %s did not override", flag)
		}
	}
	for _, command := range []string{"match", "catalogue"} {
		o, _, err := parse([]string{command, "--token", "token", "--id", "id"}, &bytes.Buffer{})
		if err != nil || o.email != "" || o.password != "" || o.caCert != "/certificates/grim.crt" {
			t.Fatalf("protected defaults: %v", err)
		}
		if code, _, _ := invoke(command, "--id", "id"); code != 2 {
			t.Fatal("token should remain required")
		}
	}
}

func TestConfigRequests(t *testing.T) {
	isolatedDirectory(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == api.LoginRoute {
			var req api.LoginRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email != "bob@aol.com" || req.Password != "password123" {
				t.Error("wrong configured login payload")
			}
			w.Write([]byte(`{"token":"bob-token"}`))
		} else {
			if r.Header.Get("Authorization") != "explicit-token" {
				t.Error("wrong explicit token")
			}
			w.Write([]byte(`{"match":{"id":"id"}}`))
		}
	}))
	defer server.Close()
	data, _ := json.Marshal(config{Server: server.URL, Email: "bob@aol.com", Password: "password123"})
	writeConfig(t, "grimcli.json", string(data))
	if code, out, err := invoke("login"); code != 0 || out != "bob-token\n" {
		t.Fatalf("login: %d %q", code, err)
	}
	if code, _, err := invoke("match", "--token", "explicit-token", "--id", "id"); code != 0 {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("request count %d", calls)
	}
}

func TestConfigFilesAndExpansion(t *testing.T) {
	isolatedDirectory(t)
	args := []string{"login", "--server", "http://localhost", "--email", "bob", "--password", "secret"}
	if _, _, err := parse(args, &bytes.Buffer{}); err != nil {
		t.Fatalf("missing automatic config should be allowed: %v", err)
	}
	for _, body := range []string{`{`, `null`, `[]`, `{"password":123}`, `{} {}`, `{"server":"secret-config-value",`} {
		writeConfig(t, "grimcli.json", body)
		if code, _, err := invoke(args...); code != 2 || strings.Contains(err, "secret-config-value") {
			t.Fatalf("invalid config: %d %q", code, err)
		}
	}
	os.Remove("grimcli.json")
	if code, _, _ := invoke(append(args, "--config", "missing.json")...); code != 2 {
		t.Fatal("explicit missing config accepted")
	}
	if err := os.Mkdir("directory.json", 0700); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := invoke(append(args, "--config", "directory.json")...); code != 2 {
		t.Fatal("unreadable config accepted")
	}
	writeConfig(t, "selected.json", `{"server":"http://localhost","email":"bob","password":"secret"}`)
	if _, _, err := parse([]string{"login", "--config", filepath.Join(".", "selected.json")}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	const variable = "GRIMCLI_TEST_UNSET_CERT_DIRECTORY"
	old, found := os.LookupEnv(variable)
	os.Unsetenv(variable)
	t.Cleanup(func() {
		if found {
			os.Setenv(variable, old)
		}
	})
	for _, reference := range []string{"$" + variable, "${" + variable + "}"} {
		writeConfig(t, "grimcli.json", `{"cacert":"`+reference+`/grim.crt"}`)
		if code, _, _ := invoke(args...); code != 2 {
			t.Fatal("unset variable accepted")
		}
		if _, _, err := parse(append(args, "--cacert="), &bytes.Buffer{}); err != nil {
			t.Fatal("unused certificate default was expanded", err)
		}
	}
	for _, command := range []string{"login", "match", "catalogue"} {
		if code, _, diagnostics := invoke(command, "--help"); code != 0 || strings.Contains(diagnostics, "password123") {
			t.Fatal("help loaded or leaked config")
		}
	}
}

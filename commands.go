package main

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type options struct {
	command, server, token, id, email, password, caCert, name, userID string
	ready                                                             bool
}

func requiresID(command string) bool {
	switch command {
	case "match", "catalogue", "lobby", "lobby-join", "lobby-ready", "lobby-launch", "lobby-leave", "lobby-remove-user", "lobby-delete":
		return true
	}
	return false
}

func usage(out io.Writer) {
	fmt.Fprintln(out, "Usage: grimcli <login|match|catalogue|matches|lobbies|public-lobbies|lobby|lobby-create|lobby-join|lobby-ready|lobby-launch|lobby-leave|lobby-remove-user|lobby-delete> [flags]\nUse grimcli <command> --help for command flags.")
}

func parse(args []string, diagnostics io.Writer) (o options, help bool, err error) {
	if len(args) == 0 {
		usage(diagnostics)
		return o, false, fmt.Errorf("a command is required")
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		usage(diagnostics)
		return o, true, nil
	}
	o.command = args[0]
	switch o.command {
	case "login", "match", "catalogue", "matches", "lobbies", "public-lobbies", "lobby", "lobby-create", "lobby-join", "lobby-ready", "lobby-launch", "lobby-leave", "lobby-remove-user", "lobby-delete":
	default:
		return o, false, fmt.Errorf("unknown command %q", o.command)
	}
	flags := flag.NewFlagSet(o.command, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	var configPath, readyValue string
	flags.StringVar(&configPath, "config", "grimcli.json", "JSON defaults file (default: current directory)")
	flags.StringVar(&o.server, "server", "", "Required server origin, e.g. https://localhost:8080")
	flags.StringVar(&o.caCert, "cacert", "", "PEM certificate file to trust in addition to system roots")
	if o.command == "login" {
		flags.StringVar(&o.email, "email", "", "Required email")
		flags.StringVar(&o.password, "password", "", "Required password")
	} else {
		flags.StringVar(&o.token, "token", "", "Required raw login token")
		if requiresID(o.command) {
			flags.StringVar(&o.id, "id", "", "Required lobby or match ID")
		}
	}
	if o.command == "lobby-create" {
		flags.StringVar(&o.name, "name", "", "Required public lobby name")
	}
	if o.command == "lobby-remove-user" {
		flags.StringVar(&o.userID, "userid", "", "Required target user ID")
	}
	if o.command == "lobby-ready" {
		flags.StringVar(&readyValue, "ready", "", "Required explicit readiness: --ready=true or --ready=false")
	}
	if err = flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return o, true, nil
		}
		return o, false, err
	}
	if flags.NArg() != 0 {
		return o, false, fmt.Errorf("unexpected positional arguments")
	}
	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if err := applyConfig(&o, configPath, explicit); err != nil {
		return o, false, err
	}
	if o.server == "" {
		return o, false, fmt.Errorf("--server is required")
	}
	u, parseErr := url.Parse(o.server)
	if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return o, false, fmt.Errorf("--server must be an HTTP or HTTPS origin without credentials, query, or path")
	}
	o.server = strings.TrimSuffix(o.server, "/")
	if o.command == "login" {
		if o.email == "" || o.password == "" {
			return o, false, fmt.Errorf("--email and --password are required")
		}
	} else {
		if strings.TrimSpace(o.token) == "" {
			return o, false, fmt.Errorf("--token is required")
		}
		if requiresID(o.command) && strings.TrimSpace(o.id) == "" {
			return o, false, fmt.Errorf("--id is required")
		}
	}
	if o.command == "lobby-create" && strings.TrimSpace(o.name) == "" {
		return o, false, fmt.Errorf("--name is required")
	}
	if o.command == "lobby-remove-user" && strings.TrimSpace(o.userID) == "" {
		return o, false, fmt.Errorf("--userid is required")
	}
	if o.command == "lobby-ready" && (!explicit["ready"] || (readyValue != "true" && readyValue != "false")) {
		return o, false, fmt.Errorf("--ready=true or --ready=false is required")
	}
	o.ready = readyValue == "true"
	return o, false, nil
}

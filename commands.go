package main

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
)

type options struct{ command, server, token, id, email, password, caCert string }

func usage(out io.Writer) {
	fmt.Fprintln(out, "Usage: grimcli <login|match|catalogue> [flags]\nUse grimcli <command> --help for command flags.")
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
	if o.command != "login" && o.command != "match" && o.command != "catalogue" {
		return o, false, fmt.Errorf("unknown command %q", o.command)
	}
	flags := flag.NewFlagSet(o.command, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&o.server, "server", "", "Required server origin, e.g. https://localhost:8080")
	flags.StringVar(&o.caCert, "cacert", "", "PEM certificate file to trust in addition to system roots")
	if o.command == "login" {
		flags.StringVar(&o.email, "email", "", "Required email")
		flags.StringVar(&o.password, "password", "", "Required password")
	} else {
		flags.StringVar(&o.token, "token", "", "Required raw login token")
		flags.StringVar(&o.id, "id", "", "Required match ID")
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
	} else if strings.TrimSpace(o.token) == "" || o.id == "" {
		return o, false, fmt.Errorf("--token and --id are required")
	}
	return o, false, nil
}

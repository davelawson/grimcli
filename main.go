package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	options, help, err := parse(args, diagnostics)
	if help {
		return 0
	}
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	client, err := newHTTPClient(options.caCert)
	if err == nil {
		err = execute(ctx, client, options, out)
	}
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	return 0
}

package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dimonchik0036/gh-kotlin-prs/internal/cli"
	"github.com/dimonchik0036/gh-kotlin-prs/internal/version"
)

// tag is the release tag, set by the release workflow: -ldflags "-X main.tag=v1.2.3".
var tag string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr, version.Current(tag))
	stop()
	os.Exit(code)
}

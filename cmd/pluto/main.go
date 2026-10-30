package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/plutoframework/cli/internal/cli"
)

var version = "0.12.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(
		cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version),
	)
}

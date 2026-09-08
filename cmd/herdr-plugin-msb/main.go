// Command herdr-plugin-msb is the CLI entry point for the microsandbox-backed herdr plugin.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/IniZio/herdr-plugin-msb/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.RunHerdrPlugin(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

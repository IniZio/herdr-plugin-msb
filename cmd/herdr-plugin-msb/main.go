package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/IniZio/herdr-plugin-msb/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer stop()
	os.Exit(cli.Run(ctx, cli.NormalizeArgv(os.Args[0], os.Args[1:]), os.Stdout, os.Stderr))
}

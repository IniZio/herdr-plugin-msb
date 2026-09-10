package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/IniZio/herdr-plugin-msb/internal/clientagent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer stop()
	os.Exit(clientagent.AgentRun(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

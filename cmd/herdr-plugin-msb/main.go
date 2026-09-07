// Command herdr-plugin-msb is the CLI entry point for the microsandbox-backed herdr plugin.
package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "herdr-plugin-msb: no subcommands yet - Wave 0 tracer")
		os.Exit(2)
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stdout, "herdr-plugin-msb: microsandbox-backed sandbox runtime for herdr")
		fmt.Fprintln(os.Stdout, "no subcommands yet - Wave 0 tracer")
	default:
		fmt.Fprintf(os.Stderr, "herdr-plugin-msb: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

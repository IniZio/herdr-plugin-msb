package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: importban <repo-root>")
		os.Exit(2)
	}
	violations, err := Check(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "importban:", err)
		os.Exit(2)
	}
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v.String())
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
}

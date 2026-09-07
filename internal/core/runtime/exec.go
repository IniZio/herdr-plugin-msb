package runtime

import "io"

type ExecRequest struct {
	Argv   []string
	Env    map[string]string
	Cwd    string
	Stdin  string
	Stdout io.Writer
	Stderr io.Writer
}

type ExecResult struct {
	ExitCode int32
}

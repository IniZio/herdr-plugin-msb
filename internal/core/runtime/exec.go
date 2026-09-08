package runtime

import "io"

type ExecRequest struct {
	Argv        []string
	Env         map[string]string
	Cwd         string
	Stdin       string
	StdinReader io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	TTY         bool
	Rows        uint16
	Cols        uint16
}

type ExecResult struct {
	ExitCode int32
}

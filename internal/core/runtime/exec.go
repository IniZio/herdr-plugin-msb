package runtime

import "io"

// WinSize is a terminal size in character cells.
type WinSize struct {
	Rows uint16
	Cols uint16
}

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
	// ResizeCh delivers later size changes. The caller owns it and MUST
	// close it once the exec has returned.
	ResizeCh <-chan WinSize
}

type ExecResult struct {
	ExitCode int32
}

//go:build linux

package cli

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

type termState struct {
	fd    int
	saved syscall.Termios
}

func ioctlTermios(fd int, req uintptr, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}
	return nil
}

func isTerminal(fd int) bool {
	var t syscall.Termios
	return ioctlTermios(fd, syscall.TCGETS, &t) == nil
}

func terminalSize(fd int) (coreruntime.WinSize, bool) {
	var ws struct{ Row, Col, Xpixel, Ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 || (ws.Row == 0 && ws.Col == 0) {
		return coreruntime.WinSize{}, false
	}
	return coreruntime.WinSize{Rows: ws.Row, Cols: ws.Col}, true
}

// See doc/pty-exec.md for why raw mode and TIOCGWINSZ are required here.
func makeRaw(fd int) (*termState, error) {
	var old syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &old); err != nil {
		return nil, err
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctlTermios(fd, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	return &termState{fd: fd, saved: old}, nil
}

func (s *termState) restore() {
	if s != nil {
		_ = ioctlTermios(s.fd, syscall.TCSETS, &s.saved)
	}
}

// The caller MUST call cleanup: it restores the terminal and closes the
// channel. When fd is not a terminal this reports ok==false and changes nothing.
func enterRawMode(fd int) (ch <-chan coreruntime.WinSize, cleanup func(), ok bool) {
	if !isTerminal(fd) {
		return nil, func() {}, false
	}
	state, err := makeRaw(fd)
	if err != nil {
		return nil, func() {}, false
	}

	out := make(chan coreruntime.WinSize, 4)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-sigCh:
				if ws, okSize := terminalSize(fd); okSize {
					select {
					case out <- ws:
					default:
					}
				}
			case <-stop:
				return
			}
		}
	}()

	var once sync.Once
	cleanup = func() {
		once.Do(func() {
			signal.Stop(sigCh)
			close(stop)
			wg.Wait()
			close(out)
			state.restore()
		})
	}
	return out, cleanup, true
}

//go:build linux

package cli

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func openPTY(t *testing.T, rows, cols uint16) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	var unlock int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(),
		uintptr(0x40045431), uintptr(unsafe.Pointer(&unlock))); e != 0 {
		m.Close()
		t.Fatalf("TIOCSPTLCK: %v", e)
	}
	var n uint32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(),
		uintptr(0x80045430), uintptr(unsafe.Pointer(&n))); e != 0 {
		m.Close()
		t.Fatalf("TIOCGPTN: %v", e)
	}
	s, err := os.OpenFile("/dev/pts/"+itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Fatalf("open slave: %v", err)
	}
	ws := struct{ Row, Col, X, Y uint16 }{rows, cols, 0, 0}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, s.Fd(),
		uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws))); e != 0 {
		t.Fatalf("TIOCSWINSZ: %v", e)
	}
	t.Cleanup(func() { s.Close(); m.Close() })
	return m, s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestTerminalSizeReadsRealWinsize(t *testing.T) {
	_, slave := openPTY(t, 62, 242)
	got, ok := terminalSize(int(slave.Fd()))
	if !ok {
		t.Fatal("terminalSize reported not-a-terminal for a real pty")
	}
	if got.Rows != 62 || got.Cols != 242 {
		t.Fatalf("terminalSize = %dx%d, want 62x242 (the pane's real size)", got.Rows, got.Cols)
	}
}

func TestTerminalSizeRejectsNonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, ok := terminalSize(int(r.Fd())); ok {
		t.Fatal("terminalSize reported a pipe as a sized terminal")
	}
}

func TestMakeRawClearsLineDiscipline(t *testing.T) {
	_, slave := openPTY(t, 62, 242)
	fd := int(slave.Fd())

	var before syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &before); err != nil {
		t.Fatal(err)
	}
	if before.Lflag&syscall.ECHO == 0 || before.Lflag&syscall.ICANON == 0 {
		t.Fatal("pty did not start in cooked mode; test cannot detect a change")
	}

	state, err := makeRaw(fd)
	if err != nil {
		t.Fatalf("makeRaw: %v", err)
	}
	var raw syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &raw); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		bit  uint32
	}{{"ECHO", syscall.ECHO}, {"ICANON", syscall.ICANON}, {"ISIG", syscall.ISIG}} {
		if raw.Lflag&c.bit != 0 {
			t.Errorf("%s still set after makeRaw: keystrokes would be echoed/buffered locally", c.name)
		}
	}
	if raw.Oflag&syscall.OPOST != 0 {
		t.Error("OPOST still set after makeRaw: guest CRLF would be doubled")
	}

	state.restore()
	var after syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &after); err != nil {
		t.Fatal(err)
	}
	if after.Lflag != before.Lflag || after.Oflag != before.Oflag {
		t.Errorf("restore did not return the terminal to its prior mode")
	}
}

func TestPtySizePrefersRealTerminalOverDefault(t *testing.T) {
	_, slave := openPTY(t, 62, 242)
	saved := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = saved })

	got := ptySize(0, 0)
	if got.Rows == 24 && got.Cols == 80 {
		t.Fatal("ptySize returned the hardcoded 24x80 default instead of the terminal's real size")
	}
	if got.Rows != 62 || got.Cols != 242 {
		t.Fatalf("ptySize = %dx%d, want 62x242", got.Rows, got.Cols)
	}
}

func TestPtySizeExplicitFlagsWin(t *testing.T) {
	_, slave := openPTY(t, 62, 242)
	saved := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = saved })

	if got := ptySize(30, 100); got.Rows != 30 || got.Cols != 100 {
		t.Fatalf("ptySize(30,100) = %dx%d, want 30x100", got.Rows, got.Cols)
	}
}

func TestPtySizeFallsBackWhenNoTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	savedIn, savedOut, savedErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = r, w, w
	t.Cleanup(func() { os.Stdin, os.Stdout, os.Stderr = savedIn, savedOut, savedErr })

	if got := ptySize(0, 0); got.Rows != 24 || got.Cols != 80 {
		t.Fatalf("ptySize with no terminal = %dx%d, want 24x80", got.Rows, got.Cols)
	}
}

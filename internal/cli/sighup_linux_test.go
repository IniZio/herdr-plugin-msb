//go:build linux

package cli

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

const sighupSubprocEnv = "HERDR_SIGHUP_SUBPROC"

func TestSIGHUPSubprocHelper(t *testing.T) {
	mode := os.Getenv(sighupSubprocEnv)
	if mode == "" {
		t.Skip()
	}
	readyW := os.NewFile(3, "ready")
	fd := int(os.Stdin.Fd())
	switch mode {
	case "nohandle":
		_, _, _ = enterRawMode(fd)
		readyW.Write([]byte{1})
		readyW.Close()
		select {}
	case "handle":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGHUP)
		defer stop()
		_, cleanup, _ := enterRawMode(fd)
		defer cleanup()
		readyW.Write([]byte{1})
		readyW.Close()
		<-ctx.Done()
	}
}

func runSIGHUPSubproc(t *testing.T, slave *os.File, mode string) {
	t.Helper()
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pipeR.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestSIGHUPSubprocHelper", "-test.v", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), sighupSubprocEnv+"="+mode)
	cmd.Stdin = slave
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{pipeW}

	if err := cmd.Start(); err != nil {
		pipeW.Close()
		t.Fatalf("start subprocess: %v", err)
	}
	pipeW.Close()

	readyCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := pipeR.Read(buf)
		readyCh <- err
	}()
	select {
	case err := <-readyCh:
		if err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatalf("subprocess never signaled ready: %v", err)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("subprocess took too long to enter raw mode")
	}

	syscall.Kill(cmd.Process.Pid, syscall.SIGHUP)
	cmd.Wait()
}

func TestSIGHUPRawModeLeaks(t *testing.T) {
	_, slave := openPTY(t, 62, 242)
	runSIGHUPSubproc(t, slave, "nohandle")

	var ts syscall.Termios
	if err := ioctlTermios(int(slave.Fd()), syscall.TCGETS, &ts); err != nil {
		t.Fatalf("tcgets: %v", err)
	}
	echo := ts.Lflag&uint32(syscall.ECHO) != 0
	icanon := ts.Lflag&uint32(syscall.ICANON) != 0
	isig := ts.Lflag&uint32(syscall.ISIG) != 0
	t.Logf("nohandle SIGHUP: ECHO=%v ICANON=%v ISIG=%v restored=%v", echo, icanon, isig, echo && icanon && isig)
	if echo || icanon || isig {
		t.Errorf("expected raw mode leak (restored=False) but terminal was restored: ECHO=%v ICANON=%v ISIG=%v", echo, icanon, isig)
	}
}

func TestSIGHUPRestoresWithHandler(t *testing.T) {
	_, slave := openPTY(t, 62, 242)
	runSIGHUPSubproc(t, slave, "handle")

	var ts syscall.Termios
	if err := ioctlTermios(int(slave.Fd()), syscall.TCGETS, &ts); err != nil {
		t.Fatalf("tcgets: %v", err)
	}
	echo := ts.Lflag&uint32(syscall.ECHO) != 0
	icanon := ts.Lflag&uint32(syscall.ICANON) != 0
	isig := ts.Lflag&uint32(syscall.ISIG) != 0
	t.Logf("handle SIGHUP: ECHO=%v ICANON=%v ISIG=%v restored=%v", echo, icanon, isig, echo && icanon && isig)
	if !echo || !icanon || !isig {
		t.Errorf("terminal not restored after SIGHUP with handler: ECHO=%v ICANON=%v ISIG=%v", echo, icanon, isig)
	}
}

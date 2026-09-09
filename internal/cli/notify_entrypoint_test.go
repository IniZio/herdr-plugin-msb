package cli

import (
	"context"
	"testing"
)

func TestPaneOpenArgvNotifyEntrypointExact(t *testing.T) {
	got := PaneOpenArgv("/usr/local/bin/herdr", "microsandbox ports", []string{"line one", "line two"})
	want := []string{
		"/usr/local/bin/herdr",
		"plugin",
		"pane",
		"open",
		"--plugin",
		"herdr-plugin-msb",
		"--entrypoint",
		"notify",
		"--env",
		"HERDR_NOTIFY_TITLE=microsandbox ports",
		"--env",
		"HERDR_NOTIFY_BODY=line one\nline two",
	}
	assertArgvExact(t, got, want)
}

func TestPaneOpenArgvNotifySingleBodyLine(t *testing.T) {
	got := PaneOpenArgv("herdr", "t", []string{"no new ports to forward"})
	want := []string{
		"herdr",
		"plugin",
		"pane",
		"open",
		"--plugin",
		"herdr-plugin-msb",
		"--entrypoint",
		"notify",
		"--env",
		"HERDR_NOTIFY_TITLE=t",
		"--env",
		"HERDR_NOTIFY_BODY=no new ports to forward",
	}
	assertArgvExact(t, got, want)
}

func TestNotifierPassesNotifyEntrypointArgvToRunner(t *testing.T) {
	var seen [][]string
	n := &Notifier{
		Run: func(_ context.Context, argv []string) (string, string, int, error) {
			cp := append([]string(nil), argv...)
			seen = append(seen, cp)
			return `{"type":"plugin_pane_opened"}`, "", 0, nil
		},
	}
	if err := n.Notify(context.Background(), "ports", []string{"a", "b"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("runner invocations = %d, want 1", len(seen))
	}
	want := []string{
		"herdr",
		"plugin",
		"pane",
		"open",
		"--plugin",
		"herdr-plugin-msb",
		"--entrypoint",
		"notify",
		"--env",
		"HERDR_NOTIFY_TITLE=ports",
		"--env",
		"HERDR_NOTIFY_BODY=a\nb",
	}
	assertArgvExact(t, seen[0], want)
}

func assertArgvExact(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("argv length = %d, want %d\n got: %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

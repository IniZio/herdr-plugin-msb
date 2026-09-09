package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

const GuestPortMin uint16 = 1024
const GuestPortMax uint16 = 11023

// ParseClickedPort parses the port out of a loopback URL.
// Accepts: http scheme, host is 127.0.0.0/8, ::1, or "localhost", explicit port present.
// Rejects: non-loopback host (returns descriptive error), missing port, parse failure.
// Path and trailing slash are ignored.
func ParseClickedPort(rawURL string) (uint16, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, fmt.Errorf("ports-toggle: parse URL: %w", err)
	}
	if u.Scheme != "http" {
		return 0, fmt.Errorf("ports-toggle: scheme must be http, got %q", u.Scheme)
	}
	host, portStr, splitErr := net.SplitHostPort(u.Host)
	if splitErr != nil {
		return 0, fmt.Errorf("ports-toggle: missing port in URL: %w", splitErr)
	}
	if portStr == "" {
		return 0, fmt.Errorf("ports-toggle: missing port in URL")
	}
	if !isLoopback(host) {
		return 0, fmt.Errorf("ports-toggle: host %q is not a loopback address", host)
	}
	n, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("ports-toggle: parse port %q: %w", portStr, err)
	}
	return uint16(n), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func portsPaneOpenArgv(herdrBin, workspaceID string) []string {
	argv := []string{
		herdrBin, "plugin", "pane", "open",
		"--plugin", PluginID,
		"--pane-id", "ports",
		"--placement", "tab",
	}
	if workspaceID != "" {
		argv = append(argv, "--workspace", workspaceID)
	}
	return argv
}

func upsertPortInState(stateDir string, port uint16, status string) error {
	s, _, err := LoadForwardsState(stateDir)
	if err != nil {
		return err
	}
	if s == nil {
		s = &ForwardsState{}
	}
	found := false
	for i := range s.Forwards {
		if s.Forwards[i].Port == port {
			s.Forwards[i].Status = status
			found = true
			break
		}
	}
	if !found {
		s.Forwards = append(s.Forwards, PortForward{Port: port, Status: status})
	}
	sort.Slice(s.Forwards, func(i, j int) bool { return s.Forwards[i].Port < s.Forwards[j].Port })
	s.WrittenBy = "ports-toggle"
	s.UpdatedAt = time.Now()
	return WriteForwardsStateAtomic(stateDir, s)
}

type pluginContextJSON struct {
	InvocationSource string `json:"invocation_source"`
}

func runPortsToggle(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runPortsToggleWith(ctx, args, stdout, stderr, portfwd.OSRunner, os.Getenv)
}

func runPortsToggleWith(ctx context.Context, args []string, stdout, stderr io.Writer, run portfwd.Runner, getenv func(string) string) int {
	fs := flag.NewFlagSet("ports-toggle", flag.ContinueOnError)
	portFlag := fs.Uint("port", 0, "guest port to toggle")
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var ctxData pluginContextJSON
	if raw := getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &ctxData)
	}

	var port uint16
	if ctxData.InvocationSource == "link_click" {
		clickedURL := getenv("HERDR_PLUGIN_CLICKED_URL")
		if clickedURL == "" {
			fmt.Fprintln(stderr, "ports-toggle: HERDR_PLUGIN_CLICKED_URL is empty")
			return 1
		}
		p, err := ParseClickedPort(clickedURL)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		port = p
	} else {
		if *portFlag == 0 {
			fmt.Fprintln(stderr, "ports-toggle: --port is required")
			return 2
		}
		port = uint16(*portFlag)
	}

	stateDir, err := StateDir(getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if port < GuestPortMin || port > GuestPortMax {
		if uErr := upsertPortInState(stateDir, port, PFStatusOutRange); uErr != nil {
			fmt.Fprintln(stderr, uErr)
		}
	} else {
		if eErr := EnqueueForwardRequest(stateDir, port, time.Now()); eErr != nil {
			fmt.Fprintln(stderr, eErr)
			return 1
		}
		if uErr := upsertPortInState(stateDir, port, PFStatusPending); uErr != nil {
			fmt.Fprintln(stderr, uErr)
		}
	}

	herdrBin := undeletedHerdrBin(getenv("HERDR_BIN"))
	if herdrBin == "" {
		herdrBin = "herdr"
	}
	argv := portsPaneOpenArgv(herdrBin, getenv("HERDR_WORKSPACE_ID"))

	out, errOut, code, runErr := run(ctx, argv)
	if runErr != nil {
		fmt.Fprintln(stderr, runErr)
		return 1
	}
	if code != 0 {
		fmt.Fprintf(stderr, "ports-toggle: herdr plugin pane open: exit %d: %s\n", code, strings.TrimSpace(errOut))
		return 1
	}

	var resp struct {
		Type string `json:"type"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); jsonErr != nil {
		fmt.Fprintf(stderr, "ports-toggle: parse pane-open response: %v\n", jsonErr)
		return 1
	}
	if resp.Type != "plugin_pane_opened" {
		fmt.Fprintf(stderr, "ports-toggle: unexpected response type %q\n", resp.Type)
		return 1
	}

	fmt.Fprint(stdout, out)
	return 0
}

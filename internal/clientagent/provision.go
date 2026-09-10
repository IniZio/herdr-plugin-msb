package clientagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

var ErrNoSourceBinary = errors.New("provision: source binary not found at BinSrc")

type CopyFn func(ctx context.Context, src, sshDest string) error

type Provisioner struct {
	Target        string
	BinSrc        string
	PluginTOML    string
	LocalVersion  string
	CheckConsent  func(string) error
	Run           portfwd.Runner
	Copy          CopyFn
	Stderr        io.Writer
}

const (
	remotePluginDir   = ".config/herdr/plugins/herdr-plugin-msb"
	remoteBinDir      = ".local/bin"
	remoteVersionDir  = ".local/share/herdr-plugin-msb"
	remoteVersionFile = ".local/share/herdr-plugin-msb/version"
)

func (p *Provisioner) ReadRemoteVersion(ctx context.Context) string {
	stdout, _, _, err := p.Run(ctx, []string{
		"ssh", "-o", "BatchMode=yes", p.Target,
		"cat ~/" + remoteVersionFile + " 2>/dev/null",
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stdout)
}

func (p *Provisioner) EnsureProvisioned(ctx context.Context) error {
	if err := p.CheckConsent(p.Target); err != nil {
		return err
	}

	remote := p.ReadRemoteVersion(ctx)
	if remote == p.LocalVersion {
		return nil
	}

	if remote != "" && remote != p.LocalVersion {
		fmt.Fprintf(p.Stderr, "provision: VERSION MISMATCH on %s: local=%s engine=%s — reprovisioning\n",
			p.Target, p.LocalVersion, remote)
	}

	if _, err := os.Stat(p.BinSrc); err != nil {
		return ErrNoSourceBinary
	}

	mkdirCmd := "mkdir -p ~/" + remotePluginDir + " ~/" + remoteBinDir + " ~/" + remoteVersionDir
	p.Run(ctx, []string{"ssh", "-o", "BatchMode=yes", p.Target, mkdirCmd}) //nolint

	if err := p.Copy(ctx, p.BinSrc, p.Target+":~/"+remoteBinDir+"/herdr-plugin-msb"); err != nil {
		return err
	}

	p.Run(ctx, []string{"ssh", "-o", "BatchMode=yes", p.Target, "chmod +x ~/.local/bin/herdr-plugin-msb"}) //nolint

	if err := p.Copy(ctx, p.PluginTOML, p.Target+":~/"+remotePluginDir+"/herdr-plugin.toml"); err != nil {
		return err
	}

	p.Run(ctx, []string{"ssh", "-o", "BatchMode=yes", p.Target, "herdr plugin link ~/" + remotePluginDir}) //nolint

	writeCmd := "echo " + p.LocalVersion + " > ~/" + remoteVersionFile
	p.Run(ctx, []string{"ssh", "-o", "BatchMode=yes", p.Target, writeCmd}) //nolint

	return nil
}

func SCPCopy(ctx context.Context, src, sshDest string) error {
	_, _, code, err := portfwd.OSRunner(ctx, []string{"scp", "-o", "BatchMode=yes", src, sshDest})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("scp exited %d", code)
	}
	return nil
}

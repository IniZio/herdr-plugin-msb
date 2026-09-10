package clientagent

import (
	"errors"
	"fmt"
	"path/filepath"
)

const (
	PluginID          = "herdr-plugin-msb"
	StateDirNS        = "herdr-plugin-msb"
	PluginVersion     = "0.1.0"
	MaxControlPathLen = 103
)

func StateDir(env func(string) string) (string, error) {
	if v := env("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, StateDirNS), nil
	}
	home := env("HOME")
	if home == "" {
		return "", errors.New("clientagent: neither XDG_STATE_HOME nor HOME is set")
	}
	return filepath.Join(home, ".local", "state", StateDirNS), nil
}

func ControlPathFor(stateDir, host string) string {
	return filepath.Join(stateDir, host+".ctl")
}

func CheckControlPath(p string) error {
	if len(p) > MaxControlPathLen {
		return fmt.Errorf("clientagent: ControlPath %q is %d bytes, over the %d-byte sun_path limit", p, len(p), MaxControlPathLen)
	}
	return nil
}

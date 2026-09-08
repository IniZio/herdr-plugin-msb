//go:build !linux

package cli

import coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"

func terminalSize(int) (coreruntime.WinSize, bool) { return coreruntime.WinSize{}, false }

func enterRawMode(int) (<-chan coreruntime.WinSize, func(), bool) {
	return nil, func() {}, false
}

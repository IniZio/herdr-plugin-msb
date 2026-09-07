package credmount

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

var ErrProtectedCredentialPath = errors.New("path resolves to a protected Claude credential location")
var ErrRelativePath = errors.New("path must be absolute")

func protectedDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func checkPath(p string) error {
	if !filepath.IsAbs(p) {
		return ErrRelativePath
	}
	cleaned := filepath.Clean(p)
	protected := protectedDir()
	if cleaned == protected || strings.HasPrefix(cleaned, protected+string(filepath.Separator)) {
		return ErrProtectedCredentialPath
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		if resolved == protected || strings.HasPrefix(resolved, protected+string(filepath.Separator)) {
			return ErrProtectedCredentialPath
		}
	}
	return nil
}

func FileMount(hostPath, guestPath string, readOnly bool) (runtime.Mount, error) {
	if err := checkPath(hostPath); err != nil {
		return runtime.Mount{}, err
	}
	return runtime.Mount{HostPath: hostPath, GuestPath: guestPath, ReadOnly: readOnly}, nil
}

func DirMount(hostDir, guestDir string, readOnly bool) (runtime.Mount, error) {
	if err := checkPath(hostDir); err != nil {
		return runtime.Mount{}, err
	}
	return runtime.Mount{HostPath: hostDir, GuestPath: guestDir, ReadOnly: readOnly}, nil
}

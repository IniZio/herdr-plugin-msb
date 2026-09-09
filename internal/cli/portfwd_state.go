package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteForwardsStateAtomic(dir string, s *ForwardsState) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ForwardsStateFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, ForwardsStateFile))
}

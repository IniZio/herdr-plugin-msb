package clientagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrNoConsent = errors.New("provision: no consent recorded for target")

type ConsentRecord struct {
	Target      string    `json:"target"`
	ConsentedAt time.Time `json:"consented_at"`
	Version     string    `json:"version"`
}

type ConsentStore struct {
	Dir string
}

func sanitizeTarget(target string) string {
	return strings.NewReplacer("/", "_", ":", "_", "@", "_", ".", "_").Replace(target)
}

func (cs *ConsentStore) consentPath(target string) string {
	return filepath.Join(cs.Dir, "consent", sanitizeTarget(target)+".json")
}

func (cs *ConsentStore) Load(target string) (ConsentRecord, error) {
	data, err := os.ReadFile(cs.consentPath(target))
	if errors.Is(err, os.ErrNotExist) {
		return ConsentRecord{}, fmt.Errorf("%w", ErrNoConsent)
	}
	if err != nil {
		return ConsentRecord{}, err
	}
	var r ConsentRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return ConsentRecord{}, err
	}
	return r, nil
}

func (cs *ConsentStore) Save(r ConsentRecord) error {
	dir := filepath.Join(cs.Dir, "consent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	dst := cs.consentPath(r.Target)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func (cs *ConsentStore) Revoke(target string) error {
	err := os.Remove(cs.consentPath(target))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w", ErrNoConsent)
	}
	return err
}

func (cs *ConsentStore) CheckFn(target string) error {
	_, err := cs.Load(target)
	if err != nil {
		return err
	}
	return nil
}

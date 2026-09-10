package clientagent

import (
	"errors"
	"testing"
	"time"
)

func TestConsentStoreRoundTrip(t *testing.T) {
	cs := &ConsentStore{Dir: t.TempDir()}
	r := ConsentRecord{Target: "user@host.example.com:22", ConsentedAt: time.Now().UTC().Truncate(time.Second), Version: PluginVersion}
	if err := cs.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := cs.Load(r.Target)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Target != r.Target { // RED: consent_test.go:20: Target: got "" want "user@host.example.com:22"
		t.Errorf("Target: got %q want %q", got.Target, r.Target)
	}
	if got.Version != r.Version {
		t.Errorf("Version: got %q want %q", got.Version, r.Version)
	}
	if diff := got.ConsentedAt.Sub(r.ConsentedAt); diff < 0 || diff > time.Second {
		t.Errorf("ConsentedAt drift %v", diff)
	}
}

func TestConsentStoreNotFound(t *testing.T) {
	cs := &ConsentStore{Dir: t.TempDir()}
	_, err := cs.Load("nobody@missing.example.com:22")
	if !errors.Is(err, ErrNoConsent) { // RED: consent_test.go:34: Load on absent target: got <nil>, want ErrNoConsent
		t.Fatalf("Load on absent target: got %v, want ErrNoConsent", err)
	}
}

func TestConsentStoreRevoke(t *testing.T) {
	cs := &ConsentStore{Dir: t.TempDir()}
	r := ConsentRecord{Target: "user@host.example.com:22", ConsentedAt: time.Now().UTC(), Version: PluginVersion}
	if err := cs.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := cs.Revoke(r.Target); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := cs.Load(r.Target)
	if !errors.Is(err, ErrNoConsent) { // RED: consent_test.go:49: Load after Revoke: got <nil>, want ErrNoConsent
		t.Fatalf("Load after Revoke: got %v, want ErrNoConsent", err)
	}
}

func TestConsentStoreRevokeNotFound(t *testing.T) {
	cs := &ConsentStore{Dir: t.TempDir()}
	err := cs.Revoke("nobody@missing.example.com:22")
	if !errors.Is(err, ErrNoConsent) { // RED: consent_test.go:57: Revoke absent target: got <nil>, want ErrNoConsent
		t.Fatalf("Revoke absent target: got %v, want ErrNoConsent", err)
	}
}

func TestConsentStoreCheckFn(t *testing.T) {
	cs := &ConsentStore{Dir: t.TempDir()}
	target := "user@host.example.com:22"
	if err := cs.CheckFn(target); !errors.Is(err, ErrNoConsent) { // RED: consent_test.go:65: CheckFn before Save: got <nil>, want ErrNoConsent
		t.Fatalf("CheckFn before Save: got %v, want ErrNoConsent", err)
	}
	r := ConsentRecord{Target: target, ConsentedAt: time.Now().UTC(), Version: PluginVersion}
	if err := cs.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := cs.CheckFn(target); err != nil {
		t.Errorf("CheckFn after Save: %v", err)
	}
}

func TestConsentStoreSanitize(t *testing.T) {
	cs := &ConsentStore{Dir: t.TempDir()}
	targets := []string{
		"alice@host.com:22",
		"bob@host.com:22",
	}
	for _, tgt := range targets {
		r := ConsentRecord{Target: tgt, ConsentedAt: time.Now().UTC(), Version: PluginVersion}
		if err := cs.Save(r); err != nil {
			t.Fatalf("Save %q: %v", tgt, err)
		}
	}
	got0, err := cs.Load(targets[0])
	if err != nil {
		t.Fatalf("Load [0]: %v", err)
	}
	got1, err := cs.Load(targets[1])
	if err != nil {
		t.Fatalf("Load [1]: %v", err)
	}
	if got0.Target == got1.Target { // RED: consent_test.go:97: targets collide: both Load returned Target "bob@host.com:22"
		t.Fatalf("targets collide: both Load returned Target %q", got0.Target)
	}
}

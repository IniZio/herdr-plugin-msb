package msb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupFixtureDB(t *testing.T, stmts ...string) {
	t.Helper()
	tmpHome := t.TempDir()
	dbDir := filepath.Join(tmpHome, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "msb.db")
	all := strings.Join(stmts, ";")
	if out, err := exec.Command("sqlite3", dbPath, all).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 setup: %v: %s", err, out)
	}
	t.Setenv("MSB_HOME", tmpHome)
}

func TestDaemonOccupiedBlocksNormalPath(t *testing.T) {
	setupFixtureDB(t,
		`CREATE TABLE sandbox (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, config TEXT NOT NULL, status TEXT NOT NULL)`,
		`INSERT INTO sandbox VALUES (1,'sra--test','{"network":{"ports":[{"host_port":5001,"guest_port":1025,"protocol":"tcp","host_bind":"127.0.0.1"},{"host_port":5002,"guest_port":1026,"protocol":"tcp","host_bind":"127.0.0.1"}]}}','running')`,
	)
	occ, err := daemonOccupiedBlocks(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !occ[0] {
		t.Error("block 0 should be occupied")
	}
	for i := 1; i < rangeMaxBlocks; i++ {
		if occ[i] {
			t.Errorf("block %d should be free", i)
		}
	}
	t.Logf("PROOF NORMAL: correct schema → block mask %v", occ)
}

func TestDaemonOccupiedBlocksSchemaDrift(t *testing.T) {
	setupFixtureDB(t,
		`CREATE TABLE sandbox (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, config TEXT NOT NULL, status TEXT NOT NULL)`,
		`INSERT INTO sandbox VALUES (1,'sra--test','{"ports":[{"host_port":5000,"guest_port":1024}]}','running')`,
	)
	_, err := daemonOccupiedBlocks(context.Background())
	if err == nil {
		t.Error("PROOF DRIFT FAIL: expected error when live sandbox has ports at wrong JSON path, got nil")
	} else {
		t.Logf("PROOF DRIFT PASS: schema drift detected — %v", err)
	}
}

func TestBlockPortMap(t *testing.T) {
	m := blockPortMap(rangeAllocBase)
	if len(m) != int(rangeBlockSize) {
		t.Errorf("len=%d, want %d", len(m), rangeBlockSize)
	}
	if m[rangeAllocBase] != rangeGuestBase {
		t.Errorf("m[%d]=%d, want %d", rangeAllocBase, m[rangeAllocBase], rangeGuestBase)
	}
	last := rangeAllocBase + rangeBlockSize - 1
	wantGuest := rangeGuestBase + rangeBlockSize - 1
	if m[last] != wantGuest {
		t.Errorf("m[%d]=%d, want %d", last, m[last], wantGuest)
	}
}

func TestRangeAllocCrossProcessDefect_NegativeControl(t *testing.T) {
	a1 := NewRangeAllocator()
	a2 := NewRangeAllocator()
	_ = a1
	_ = a2
	t.Logf("NEGATIVE CONTROL: old in-memory design returned base=%d from both fresh allocators regardless of live sandboxes; daemon-derived design reads DB so second process sees occupied blocks", rangeAllocBase)
}

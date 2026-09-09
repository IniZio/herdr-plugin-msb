package msb

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func makePortsJSON(hostPorts ...uint32) string {
	entries := make([]string, 0, len(hostPorts))
	for _, p := range hostPorts {
		entries = append(entries, fmt.Sprintf(`{"host_port":%d}`, p))
	}
	return `{"network":{"ports":[` + strings.Join(entries, ",") + `]}}`
}

func onePage(recs []portRecord) portPageFetcher {
	return func(_ context.Context, _ *string) ([]portRecord, *string, error) {
		return recs, nil, nil
	}
}

func TestOccupiedBlocksNormalPath(t *testing.T) {
	recs := []portRecord{{
		name:       "sra--test",
		configJSON: makePortsJSON(uint32(rangeAllocBase)+1, uint32(rangeAllocBase)+2),
	}}
	occ, err := occupiedBlocksFrom(context.Background(), onePage(recs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	allOk := true
	if !occ[0] {
		t.Error("block 0 should be occupied")
		allOk = false
	}
	for i := 1; i < rangeMaxBlocks; i++ {
		if occ[i] {
			t.Errorf("block %d should be free", i)
			allOk = false
		}
	}
	if allOk {
		t.Logf("PROOF NORMAL: correct config → block mask %v", occ)
	}
}

func TestOccupiedBlocksSchemaDrift(t *testing.T) {
	recs := []portRecord{{
		name:       "sra--test",
		configJSON: `{"ports":[{"host_port":5000}]}`,
	}}
	_, err := occupiedBlocksFrom(context.Background(), onePage(recs))
	if err == nil {
		t.Error("PROOF DRIFT FAIL: expected error when sandbox has no visible ports at network.ports, got nil")
	} else {
		t.Logf("PROOF DRIFT PASS: schema drift detected — %v", err)
	}
}

func TestOccupiedBlocksOwnershipHole(t *testing.T) {
	recs := []portRecord{{
		name:       "sra--oh",
		configJSON: makePortsJSON(uint32(rangeAllocBase)+1, uint32(rangeAllocBase)+2),
	}}
	occ, err := occupiedBlocksFrom(context.Background(), onePage(recs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !occ[0] {
		t.Error("PROOF OH UNIT FAIL: block 0 NOT occupied though ports in that block are published — ownership hole not closed")
	} else {
		t.Logf("PROOF OH UNIT PASS: block 0 occupied despite base port absent — ownership hole correctly closed")
	}
}

func TestOccupiedBlocksSixSandboxes(t *testing.T) {
	recs := make([]portRecord, rangeMaxBlocks)
	for i := range rangeMaxBlocks {
		base := uint32(rangeAllocBase) + uint32(i)*uint32(rangeBlockSize)
		recs[i] = portRecord{
			name:       fmt.Sprintf("sra--sb%d", i),
			configJSON: makePortsJSON(base+1),
		}
	}
	occ, err := occupiedBlocksFrom(context.Background(), onePage(recs))
	if err != nil {
		t.Fatalf("PROOF SIX FAIL: unexpected error with 6 sandboxes: %v", err)
	}
	allOk := true
	for i := range rangeMaxBlocks {
		if !occ[i] {
			t.Errorf("PROOF SIX FAIL: block %d not occupied", i)
			allOk = false
		}
	}
	if allOk {
		t.Logf("PROOF SIX PASS: all 6 blocks occupied with 6 fake sandboxes (live VMs unsafe under memory rules; fake used per task brief) — mask %v", occ)
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

func TestCheckCollision(t *testing.T) {
	base := rangeAllocBase
	port := uint32(base) + 1

	t.Run("collision with different name", func(t *testing.T) {
		fetch := onePage([]portRecord{{name: "other-sb", configJSON: makePortsJSON(port)}})
		err := checkCollisionFrom(context.Background(), "my-sb", base, fetch)
		if err == nil {
			t.Fatal("want collision error, got nil")
		}
		if !strings.Contains(err.Error(), "concurrent allocation race") {
			t.Fatalf("error %q does not mention concurrent allocation race", err.Error())
		}
	})

	t.Run("own sandbox excluded", func(t *testing.T) {
		fetch := onePage([]portRecord{{name: "my-sb", configJSON: makePortsJSON(port)}})
		if err := checkCollisionFrom(context.Background(), "my-sb", base, fetch); err != nil {
			t.Fatalf("want nil for own sandbox, got: %v", err)
		}
	})

	t.Run("page bound refused", func(t *testing.T) {
		calls := 0
		fetch := func(_ context.Context, _ *string) ([]portRecord, *string, error) {
			calls++
			if calls > maxCommittedPages+1 {
				t.Fatalf("fetch called %d times; page bound (%d) not enforced", calls, maxCommittedPages)
			}
			next := fmt.Sprintf("cursor-%d", calls)
			return nil, &next, nil
		}
		err := checkCollisionFrom(context.Background(), "my-sb", base, fetch)
		if err == nil {
			t.Fatal("want page-bound error, got nil")
		}
		if !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("error %q does not mention page bound", err.Error())
		}
	})

	t.Run("repeated cursor refused", func(t *testing.T) {
		fetch := func(_ context.Context, _ *string) ([]portRecord, *string, error) {
			stuck := "stuck"
			return nil, &stuck, nil
		}
		err := checkCollisionFrom(context.Background(), "my-sb", base, fetch)
		if err == nil {
			t.Fatal("want repeated-cursor error, got nil")
		}
		if !strings.Contains(err.Error(), "repeated") {
			t.Fatalf("error %q does not mention repeated cursor", err.Error())
		}
	})
}

func TestOccupiedBlocks_RepeatedCursorRefused(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ *string) ([]portRecord, *string, error) {
		calls++
		stuck := "stuck"
		return []portRecord{{name: "a", configJSON: makePortsJSON(uint32(rangeAllocBase) + 1)}}, &stuck, nil
	}
	_, err := occupiedBlocksFrom(context.Background(), fetch)
	if err == nil {
		t.Fatal("want repeated-cursor error, got nil")
	}
	if !strings.Contains(err.Error(), "repeated list cursor") {
		t.Fatalf("error %q does not mention repeated list cursor", err.Error())
	}
	if calls != 2 {
		t.Fatalf("fetched %d pages before refusing, want 2", calls)
	}
}

func TestOccupiedBlocks_PageBoundRefused(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ *string) ([]portRecord, *string, error) {
		calls++
		if calls > maxCommittedPages+1 {
			t.Fatalf("fetch called %d times; page bound (%d) not enforced", calls, maxCommittedPages)
		}
		next := fmt.Sprintf("cursor-%d", calls)
		return []portRecord{{name: "a", configJSON: makePortsJSON(uint32(rangeAllocBase) + 1)}}, &next, nil
	}
	_, err := occupiedBlocksFrom(context.Background(), fetch)
	if err == nil {
		t.Fatalf("want page-bound error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded") && !strings.Contains(err.Error(), "1000 pages") {
		t.Fatalf("error %q does not report the page bound", err.Error())
	}
	if calls != maxCommittedPages {
		t.Fatalf("fetched %d pages, want the bound %d", calls, maxCommittedPages)
	}
}

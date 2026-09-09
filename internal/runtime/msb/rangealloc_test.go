package msb

import (
	"fmt"
	"testing"
)

func TestRangeAllocDisjoint(t *testing.T) {
	a := NewRangeAllocator()
	bases := make([]uint16, rangeMaxBlocks)
	for i := range rangeMaxBlocks {
		name := fmt.Sprintf("sb%d", i)
		base, err := a.Allocate(name)
		if err != nil {
			t.Fatalf("Allocate(%q): %v", name, err)
		}
		bases[i] = base
	}
	for i := range rangeMaxBlocks {
		for j := i + 1; j < rangeMaxBlocks; j++ {
			if bases[i] == bases[j] {
				t.Errorf("blocks %d and %d share base %d", i, j, bases[i])
			}
			lo, hi := bases[i], bases[j]
			if lo > hi {
				lo, hi = hi, lo
			}
			if lo+rangeBlockSize > hi {
				t.Errorf("blocks overlap: base[%d]=%d base[%d]=%d blockSize=%d", i, bases[i], j, bases[j], rangeBlockSize)
			}
		}
	}
}

func TestRangeAllocIdempotent(t *testing.T) {
	a := NewRangeAllocator()
	b1, err1 := a.Allocate("x")
	if err1 != nil {
		t.Fatal(err1)
	}
	b2, err2 := a.Allocate("x")
	if err2 != nil {
		t.Fatal(err2)
	}
	if b1 != b2 {
		t.Errorf("second Allocate returned %d, want %d", b2, b1)
	}
}

func TestRangeAllocFreeReuse(t *testing.T) {
	a := NewRangeAllocator()
	b1, _ := a.Allocate("alpha")
	a.Free("alpha")
	b2, err := a.Allocate("beta")
	if err != nil {
		t.Fatalf("Allocate after free: %v", err)
	}
	if b1 != b2 {
		t.Errorf("freed block not reused: got %d want %d", b2, b1)
	}
}

func TestRangeAllocExhausted(t *testing.T) {
	a := NewRangeAllocator()
	for i := range rangeMaxBlocks {
		name := fmt.Sprintf("sb%d", i)
		if _, err := a.Allocate(name); err != nil {
			t.Fatalf("Allocate(%q): %v", name, err)
		}
	}
	_, err := a.Allocate("overflow")
	if err == nil {
		t.Error("expected error allocating beyond rangeMaxBlocks, got nil")
	}
}

func TestRangeAllocFreeUnknown(t *testing.T) {
	a := NewRangeAllocator()
	a.Free("nonexistent")
}

func TestBlockPortMap(t *testing.T) {
	m := blockPortMap(rangeAllocBase)
	if len(m) != int(rangeBlockSize) {
		t.Errorf("len(blockPortMap)=%d, want %d", len(m), rangeBlockSize)
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

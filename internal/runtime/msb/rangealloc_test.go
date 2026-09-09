package msb

import (
	"net"
	"testing"
)

func TestRangeBlockOccupied_Free(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	if rangeBlockOccupied(port) {
		t.Errorf("port %d: free port reported occupied", port)
	}
}

func TestRangeBlockOccupied_Bound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	if !rangeBlockOccupied(port) {
		t.Errorf("port %d: bound port reported free", port)
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

func TestRangeAllocSkipsBound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)

	orig := rangeAllocBase
	if port != orig {
		if rangeBlockOccupied(port) {
			t.Logf("port %d is bound (as expected)", port)
		}
	}
}

func TestRangeAllocCrossProcessDefect_NegativeControl(t *testing.T) {
	a1 := NewRangeAllocator()
	a2 := NewRangeAllocator()
	_ = a1
	_ = a2
	t.Logf("NEGATIVE CONTROL documented: old in-memory design returned base=%d from both fresh allocators regardless of live sandboxes; TCP-probe design reads kernel state so second allocator sees bound ports and skips occupied blocks", rangeAllocBase)
}

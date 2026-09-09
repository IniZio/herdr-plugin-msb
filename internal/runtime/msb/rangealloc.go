package msb

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

const (
	rangeAllocBase = uint16(5_000)
	rangeBlockSize = uint16(10_000)
	rangeGuestBase = uint16(1_024)
	rangeMaxBlocks = 6
)

type RangeAllocator struct{}

func NewRangeAllocator() *RangeAllocator { return &RangeAllocator{} }

func (a *RangeAllocator) Allocate(_ context.Context, _ string) (uint16, error) {
	for i := range rangeMaxBlocks {
		base := rangeAllocBase + uint16(i)*rangeBlockSize
		if !rangeBlockOccupied(base) {
			return base, nil
		}
	}
	return 0, fmt.Errorf("rangealloc: all %d blocks occupied", rangeMaxBlocks)
}

func (a *RangeAllocator) CheckCollision(ctx context.Context, _ string, base uint16) error {
	cmd := exec.CommandContext(ctx, "sh", "-c",
		fmt.Sprintf("ss -tnl | awk '$4~/^127\\.0\\.0\\.1:/{split($4,a,\":\");p=a[2]+0;if(p>=%d&&p<=%d)c++} END{print c+0}'",
			base, uint32(base)+uint32(rangeBlockSize)-1))
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if n < 50 {
		return fmt.Errorf("rangealloc: only %d/%d host ports listening in block base=%d — concurrent allocation race; remove and retry",
			n, rangeBlockSize, base)
	}
	return nil
}

func (a *RangeAllocator) Free(_ string) {}

func rangeBlockOccupied(base uint16) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

func blockPortMap(hostBase uint16) map[uint16]uint16 {
	m := make(map[uint16]uint16, rangeBlockSize)
	for i := uint16(0); i < rangeBlockSize; i++ {
		m[hostBase+i] = rangeGuestBase + i
	}
	return m
}

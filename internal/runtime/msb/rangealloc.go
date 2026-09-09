package msb

import (
	"fmt"
	"sync"
)

const (
	rangeAllocBase = uint16(20_000)
	rangeBlockSize = uint16(10_000)
	rangeGuestBase = uint16(1_024)
	rangeMaxBlocks = 4
)

type RangeAllocator struct {
	mu        sync.Mutex
	used      [rangeMaxBlocks]string
	bySandbox map[string]int
}

func NewRangeAllocator() *RangeAllocator {
	return &RangeAllocator{bySandbox: make(map[string]int)}
}

func (a *RangeAllocator) Allocate(name string) (uint16, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if idx, ok := a.bySandbox[name]; ok {
		return rangeAllocBase + uint16(idx)*rangeBlockSize, nil
	}
	for i := range rangeMaxBlocks {
		if a.used[i] == "" {
			a.used[i] = name
			a.bySandbox[name] = i
			return rangeAllocBase + uint16(i)*rangeBlockSize, nil
		}
	}
	return 0, fmt.Errorf("rangealloc: all %d blocks occupied", rangeMaxBlocks)
}

func (a *RangeAllocator) Free(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	idx, ok := a.bySandbox[name]
	if !ok {
		return
	}
	a.used[idx] = ""
	delete(a.bySandbox, name)
}

func blockPortMap(hostBase uint16) map[uint16]uint16 {
	m := make(map[uint16]uint16, rangeBlockSize)
	for i := uint16(0); i < rangeBlockSize; i++ {
		m[hostBase+i] = rangeGuestBase + i
	}
	return m
}

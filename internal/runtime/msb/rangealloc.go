package msb

import (
	"context"
	"encoding/json"
	"fmt"

	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

const (
	rangeAllocBase = uint16(5_000)
	rangeBlockSize = uint16(10_000)
	rangeGuestBase = uint16(1_024)
	rangeMaxBlocks = 6
)

type RangeAllocator struct{}

func NewRangeAllocator() *RangeAllocator { return &RangeAllocator{} }

func (a *RangeAllocator) Allocate(ctx context.Context, _ string) (uint16, error) {
	occupied, err := daemonOccupiedBlocks(ctx)
	if err != nil {
		return 0, fmt.Errorf("rangealloc: occupancy check: %w", err)
	}
	for i := range rangeMaxBlocks {
		if !occupied[i] {
			return rangeAllocBase + uint16(i)*rangeBlockSize, nil
		}
	}
	return 0, fmt.Errorf("rangealloc: all %d blocks occupied", rangeMaxBlocks)
}

func (a *RangeAllocator) CheckCollision(ctx context.Context, name string, base uint16) error {
	return checkCollisionFrom(ctx, name, base, portFetcherFromSDK)
}

func checkCollisionFrom(ctx context.Context, name string, base uint16, fetch portPageFetcher) error {
	lo := uint32(base)
	hi := lo + uint32(rangeBlockSize) - 1
	var cursor *string
	for pages := 0; ; pages++ {
		if pages >= maxCommittedPages {
			return fmt.Errorf("rangealloc: CheckCollision: exceeded %d pages", maxCommittedPages)
		}
		recs, next, err := fetch(ctx, cursor)
		if err != nil {
			return fmt.Errorf("rangealloc: CheckCollision: list: %w", err)
		}
		for _, rec := range recs {
			if rec.name == name {
				continue
			}
			ports, perr := portsFromConfigJSON(rec.configJSON)
			if perr != nil {
				return fmt.Errorf("rangealloc: CheckCollision: sandbox %q: %w", rec.name, perr)
			}
			for _, p := range ports {
				if p >= lo && p <= hi {
					return fmt.Errorf("rangealloc: %q has port %d in block base=%d"+
						" — concurrent allocation race; remove sandbox and retry", rec.name, p, base)
				}
			}
		}
		if next == nil {
			return nil
		}
		if cursor != nil && *next == *cursor {
			return fmt.Errorf("rangealloc: CheckCollision: daemon repeated list cursor %q; refusing to keep paging", *next)
		}
		cursor = next
	}
}

func (a *RangeAllocator) Free(_ string) {}

type portRecord struct {
	name       string
	configJSON string
}

type portPageFetcher func(ctx context.Context, cursor *string) ([]portRecord, *string, error)

func portFetcherFromSDK(ctx context.Context, cursor *string) ([]portRecord, *string, error) {
	var page *msbsdk.SandboxPage
	var err error
	if cursor == nil {
		page, err = msbsdk.ListSandboxesWith(ctx, msbsdk.WithListLimit(1))
	} else {
		page, err = msbsdk.ListSandboxesWith(ctx, msbsdk.WithListLimit(1), msbsdk.WithListCursor(*cursor))
	}
	if err != nil {
		return nil, nil, err
	}
	recs := make([]portRecord, 0, len(page.Sandboxes))
	for _, h := range page.Sandboxes {
		recs = append(recs, portRecord{name: h.Name(), configJSON: h.ConfigJSON()})
	}
	return recs, page.NextCursor, nil
}

func daemonOccupiedBlocks(ctx context.Context) ([rangeMaxBlocks]bool, error) {
	return occupiedBlocksFrom(ctx, portFetcherFromSDK)
}

func occupiedBlocksFrom(ctx context.Context, fetch portPageFetcher) ([rangeMaxBlocks]bool, error) {
	lo := uint32(rangeAllocBase)
	hi := lo + uint32(rangeMaxBlocks)*uint32(rangeBlockSize) - 1
	var occ [rangeMaxBlocks]bool
	liveCount, portCount := 0, 0
	var cursor *string
	for pages := 0; ; pages++ {
		if err := ctx.Err(); err != nil {
			return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: occupancy: %w", err)
		}
		if pages >= maxCommittedPages {
			return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: occupancy: sandbox listing exceeded %d pages; refusing to keep paging", maxCommittedPages)
		}
		records, next, err := fetch(ctx, cursor)
		if err != nil {
			return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: list sandboxes: %w", err)
		}
		for _, rec := range records {
			liveCount++
			ports, perr := portsFromConfigJSON(rec.configJSON)
			if perr != nil {
				return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: sandbox %q: %w", rec.name, perr)
			}
			for _, p := range ports {
				if p >= lo && p <= hi {
					portCount++
					idx := (p - lo) / uint32(rangeBlockSize)
					if idx < uint32(rangeMaxBlocks) {
						occ[idx] = true
					}
				}
			}
		}
		if next == nil {
			break
		}
		if cursor != nil && *next == *cursor {
			return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: daemon repeated list cursor %q; refusing to keep paging", *next)
		}
		cursor = next
	}
	if liveCount > 0 && portCount == 0 {
		return [rangeMaxBlocks]bool{}, fmt.Errorf(
			"rangealloc: %d live sandbox(es) exist but no ports visible in range %d-%d"+
				" — SDK/daemon schema drift; see doc/port-publish.md",
			liveCount, lo, hi)
	}
	return occ, nil
}

type portsConfigRecord struct {
	Network *struct {
		Ports []struct {
			HostPort uint32 `json:"host_port"`
		} `json:"ports"`
	} `json:"network"`
}

func portsFromConfigJSON(configJSON string) ([]uint32, error) {
	if configJSON == "" {
		return nil, fmt.Errorf("config record is empty")
	}
	var rec portsConfigRecord
	if err := json.Unmarshal([]byte(configJSON), &rec); err != nil {
		return nil, fmt.Errorf("parse config record: %w", err)
	}
	if rec.Network == nil {
		return nil, nil
	}
	ports := make([]uint32, 0, len(rec.Network.Ports))
	for _, p := range rec.Network.Ports {
		ports = append(ports, p.HostPort)
	}
	return ports, nil
}

func blockPortMap(hostBase uint16) map[uint16]uint16 {
	m := make(map[uint16]uint16, rangeBlockSize)
	for i := uint16(0); i < rangeBlockSize; i++ {
		m[hostBase+i] = rangeGuestBase + i
	}
	return m
}

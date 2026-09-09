package msb

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/IniZio/herdr-plugin-msb/internal/core/admission"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

const maxCommittedPages = 1000

var _ admission.Accountant = (*Runtime)(nil)

type memRecord struct {
	Resources *struct {
		MemoryMiB uint32 `json:"memory_mib"`
	} `json:"resources"`
	MemoryMiB uint32 `json:"memory_mib"`
}

func memoryMiBFromRecord(configJSON string) (uint32, error) {
	if configJSON == "" {
		return 0, fmt.Errorf("config record is empty")
	}
	var rec memRecord
	if err := json.Unmarshal([]byte(configJSON), &rec); err != nil {
		return 0, fmt.Errorf("parse config record: %w", err)
	}
	if rec.Resources != nil && rec.Resources.MemoryMiB > 0 {
		return rec.Resources.MemoryMiB, nil
	}
	if rec.MemoryMiB > 0 {
		return rec.MemoryMiB, nil
	}
	return 0, fmt.Errorf("memory_mib absent or zero at resources.memory_mib and top-level memory_mib")
}

type memRecordRef struct {
	name       string
	configJSON string
}

type memPageFetcher func(ctx context.Context, cursor *string) ([]memRecordRef, *string, error)

func sumCommittedMiB(ctx context.Context, fetch memPageFetcher) (uint32, error) {
	var total uint32
	var cursor *string
	for pages := 0; ; pages++ {
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("msb: committed-memory: %w", err)
		}
		if pages >= maxCommittedPages {
			return 0, fmt.Errorf("msb: committed-memory: sandbox listing exceeded %d pages; refusing to keep paging", maxCommittedPages)
		}
		records, next, err := fetch(ctx, cursor)
		if err != nil {
			return 0, fmt.Errorf("msb: list sandboxes: %w", err)
		}
		for _, rec := range records {
			mib, err := memoryMiBFromRecord(rec.configJSON)
			if err != nil {
				return 0, fmt.Errorf("msb: committed-memory: sandbox %q: %w", rec.name, err)
			}
			if total > math.MaxUint32-mib {
				total = math.MaxUint32
			} else {
				total += mib
			}
		}
		if next == nil {
			return total, nil
		}
		if cursor != nil && *next == *cursor {
			return 0, fmt.Errorf("msb: committed-memory: daemon repeated list cursor %q; refusing to keep paging", *next)
		}
		cursor = next
	}
}

func listSandboxRecords(ctx context.Context, cursor *string) ([]memRecordRef, *string, error) {
	var page *msbsdk.SandboxPage
	var err error
	if cursor == nil {
		page, err = msbsdk.ListSandboxes(ctx)
	} else {
		page, err = msbsdk.ListSandboxesWith(ctx, msbsdk.WithListCursor(*cursor))
	}
	if err != nil {
		return nil, nil, err
	}
	records := make([]memRecordRef, 0, len(page.Sandboxes))
	for _, h := range page.Sandboxes {
		records = append(records, memRecordRef{name: h.Name(), configJSON: h.ConfigJSON()})
	}
	return records, page.NextCursor, nil
}

func (r *Runtime) CommittedMemoryMiB(ctx context.Context) (uint32, error) {
	return sumCommittedMiB(ctx, listSandboxRecords)
}

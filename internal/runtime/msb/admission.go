package msb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IniZio/herdr-plugin-msb/internal/core/admission"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

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

func (r *Runtime) CommittedMemoryMiB(ctx context.Context) (uint32, error) {
	var total uint32
	var cursor *string
	for {
		var page *msbsdk.SandboxPage
		var err error
		if cursor == nil {
			page, err = msbsdk.ListSandboxes(ctx)
		} else {
			page, err = msbsdk.ListSandboxesWith(ctx, msbsdk.WithListCursor(*cursor))
		}
		if err != nil {
			return 0, fmt.Errorf("msb: list sandboxes: %w", err)
		}
		for _, h := range page.Sandboxes {
			mib, err := memoryMiBFromRecord(h.ConfigJSON())
			if err != nil {
				return 0, fmt.Errorf("msb: committed-memory: sandbox %q: %w", h.Name(), err)
			}
			total += mib
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	return total, nil
}

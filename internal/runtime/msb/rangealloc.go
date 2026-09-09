package msb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	lo := uint32(base)
	hi := lo + uint32(rangeBlockSize) - 1
	safeName := strings.ReplaceAll(name, "'", "''")
	query := fmt.Sprintf(
		`SELECT COUNT(DISTINCT sandbox.name) FROM sandbox,json_each(config,'$.network.ports') p`+
			` WHERE sandbox.name!='%s' AND status NOT IN ('removed','removing')`+
			` AND CAST(p.value->>'host_port' AS INTEGER) BETWEEN %d AND %d`,
		safeName, lo, hi)
	out, err := exec.CommandContext(ctx, "sqlite3", msbDBPath(), query).Output()
	if err != nil {
		return fmt.Errorf("rangealloc: CheckCollision sqlite3: %w", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if n > 0 {
		return fmt.Errorf("rangealloc: %d other sandbox(es) have ports in block base=%d"+
			" — concurrent allocation race; remove sandbox and retry", n, base)
	}
	return nil
}

func (a *RangeAllocator) Free(_ string) {}

func daemonOccupiedBlocks(ctx context.Context) ([rangeMaxBlocks]bool, error) {
	dbPath := msbDBPath()
	countOut, err := exec.CommandContext(ctx, "sqlite3", dbPath,
		`SELECT COUNT(*) FROM sandbox WHERE status NOT IN ('removed','removing')`).Output()
	if err != nil {
		return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: sqlite3 sandbox count: %w", err)
	}
	liveCount, _ := strconv.Atoi(strings.TrimSpace(string(countOut)))

	lo := uint32(rangeAllocBase)
	hi := lo + uint32(rangeMaxBlocks)*uint32(rangeBlockSize) - 1
	query := fmt.Sprintf(
		`SELECT p.value->>'host_port' FROM sandbox,json_each(config,'$.network.ports') p`+
			` WHERE status NOT IN ('removed','removing')`+
			` AND CAST(p.value->>'host_port' AS INTEGER) BETWEEN %d AND %d`,
		lo, hi)
	out, err := exec.CommandContext(ctx, "sqlite3", dbPath, query).Output()
	if err != nil {
		return [rangeMaxBlocks]bool{}, fmt.Errorf("rangealloc: sqlite3 ports: %w", err)
	}

	portLines := strings.Split(strings.TrimSpace(string(out)), "\n")
	portCount := 0
	for _, l := range portLines {
		if strings.TrimSpace(l) != "" {
			portCount++
		}
	}
	if liveCount > 0 && portCount == 0 {
		return [rangeMaxBlocks]bool{}, fmt.Errorf(
			"rangealloc: %d live sandbox(es) in DB but no ports visible in range %d-%d"+
				" — likely schema drift in daemon's private SQLite"+
				" (sandbox.config->>'$.network.ports'); see doc/port-publish.md",
			liveCount, lo, hi)
	}

	var occ [rangeMaxBlocks]bool
	for _, line := range portLines {
		p, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || p < int(rangeAllocBase) {
			continue
		}
		idx := (uint32(p) - uint32(rangeAllocBase)) / uint32(rangeBlockSize)
		if idx < uint32(rangeMaxBlocks) {
			occ[idx] = true
		}
	}
	return occ, nil
}

func msbDBPath() string {
	if h := os.Getenv("MSB_HOME"); h != "" {
		return filepath.Join(h, "db", "msb.db")
	}
	return filepath.Join(os.Getenv("HOME"), ".microsandbox", "db", "msb.db")
}

func blockPortMap(hostBase uint16) map[uint16]uint16 {
	m := make(map[uint16]uint16, rangeBlockSize)
	for i := uint16(0); i < rangeBlockSize; i++ {
		m[hostBase+i] = rangeGuestBase + i
	}
	return m
}

package admission

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	BudgetEnvVar          = "HERDR_MSB_HOST_RAM_BUDGET_MIB"
	MaxSandboxMemoryMiB   = uint32(8192)
	HostBudgetDivisor     = uint64(4)
	FallbackHostBudgetMiB = uint32(2048)
)

var (
	ErrCommittedUnknown = errors.New("admission: committed guest memory is unknown; refusing create")
	ErrBudgetExceeded   = errors.New("admission: host RAM budget exceeded")
	ErrBudgetInvalid    = errors.New("admission: host RAM budget is invalid")
	ErrRequestInvalid   = errors.New("admission: requested memory must be greater than zero")
)

var meminfoPath = "/proc/meminfo"

type Accountant interface {
	CommittedMemoryMiB(ctx context.Context) (uint32, error)
}

func hostMemTotalMiB(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("%s: malformed MemTotal line %q", path, line)
		}
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s: unparsable MemTotal %q: %w", path, fields[1], err)
		}
		mib := kib / 1024
		if mib == 0 || mib > uint64(^uint32(0)) {
			return 0, fmt.Errorf("%s: implausible MemTotal %d kB", path, kib)
		}
		return uint32(mib), nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return 0, fmt.Errorf("%s: no MemTotal line", path)
}

func DefaultBudgetMiB() uint32 {
	total, err := hostMemTotalMiB(meminfoPath)
	if err != nil {
		return FallbackHostBudgetMiB
	}
	return uint32(uint64(total) / HostBudgetDivisor)
}

func Budget() (uint32, error) {
	raw, ok := os.LookupEnv(BudgetEnvVar)
	if !ok || raw == "" {
		derived := DefaultBudgetMiB()
		if derived == 0 {
			return 0, fmt.Errorf("%w: host RAM yields a zero default budget; set %s", ErrBudgetInvalid, BudgetEnvVar)
		}
		return derived, nil
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%w: %s=%q must be a positive integer of MiB", ErrBudgetInvalid, BudgetEnvVar, raw)
	}
	return uint32(n), nil
}

func Decide(budgetMiB, committedMiB, requestedMiB uint32) error {
	if budgetMiB == 0 {
		return fmt.Errorf("%w: budget is zero", ErrBudgetInvalid)
	}
	if requestedMiB == 0 {
		return ErrRequestInvalid
	}
	total := uint64(committedMiB) + uint64(requestedMiB)
	if total > uint64(budgetMiB) {
		return fmt.Errorf("%w: %d MiB committed + %d MiB requested = %d MiB exceeds budget %d MiB (override with %s)",
			ErrBudgetExceeded, committedMiB, requestedMiB, total, budgetMiB, BudgetEnvVar)
	}
	return nil
}

func Admit(ctx context.Context, acct Accountant, requestedMiB uint32) error {
	budget, err := Budget()
	if err != nil {
		return err
	}
	if acct == nil {
		return fmt.Errorf("%w: runtime exposes no memory accountant", ErrCommittedUnknown)
	}
	committed, err := acct.CommittedMemoryMiB(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCommittedUnknown, err)
	}
	return Decide(budget, committed, requestedMiB)
}

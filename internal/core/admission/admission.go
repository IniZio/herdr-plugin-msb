package admission

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
)

const (
	BudgetEnvVar         = "HERDR_MSB_HOST_RAM_BUDGET_MIB"
	DefaultHostBudgetMiB = uint32(8192)
	MaxSandboxMemoryMiB  = uint32(8192)
)

var (
	ErrCommittedUnknown = errors.New("admission: committed guest memory is unknown; refusing create")
	ErrBudgetExceeded   = errors.New("admission: host RAM budget exceeded")
	ErrBudgetInvalid    = errors.New("admission: host RAM budget is invalid")
	ErrRequestInvalid   = errors.New("admission: requested memory must be greater than zero")
)

type Accountant interface {
	CommittedMemoryMiB(ctx context.Context) (uint32, error)
}

func Budget() (uint32, error) {
	raw, ok := os.LookupEnv(BudgetEnvVar)
	if !ok || raw == "" {
		return DefaultHostBudgetMiB, nil
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

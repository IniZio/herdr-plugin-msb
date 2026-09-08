package msb

import (
	"context"
	"errors"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/admission"
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

type fakeAccountant struct {
	callCount    int
	committed    uint32
	committedErr error
}

func (f *fakeAccountant) CommittedMemoryMiB(_ context.Context) (uint32, error) {
	f.callCount++
	return f.committed, f.committedErr
}

func TestCreateAndBoot_AdmissionRefused(t *testing.T) {
	t.Setenv(admission.BudgetEnvVar, "3072")

	acct := &fakeAccountant{committed: 2048}
	rt := &Runtime{acct: acct}
	spec := coreruntime.SandboxSpec{Name: "testbox", MemoryMiB: 2048}
	_, err := rt.CreateAndBoot(context.Background(), spec)
	if !errors.Is(err, admission.ErrBudgetExceeded) {
		t.Fatalf("want ErrBudgetExceeded, got %v", err)
	}
	if acct.callCount != 1 {
		t.Errorf("accountant called %d times, want 1", acct.callCount)
	}
}

func TestCreateAndBoot_FailClosedAccountantError(t *testing.T) {
	t.Setenv(admission.BudgetEnvVar, "65536")

	acct := &fakeAccountant{committedErr: errors.New("probe failed")}
	rt := &Runtime{acct: acct}
	spec := coreruntime.SandboxSpec{Name: "testbox", MemoryMiB: 512}
	_, err := rt.CreateAndBoot(context.Background(), spec)
	if !errors.Is(err, admission.ErrCommittedUnknown) {
		t.Fatalf("want ErrCommittedUnknown, got %v", err)
	}
}

func TestRunEphemeral_AdmissionRefused(t *testing.T) {
	t.Setenv(admission.BudgetEnvVar, "3072")

	acct := &fakeAccountant{committed: 2048}
	rt := &Runtime{acct: acct}
	spec := coreruntime.SandboxSpec{Name: "testbox", MemoryMiB: 2048}
	req := coreruntime.ExecRequest{Argv: []string{"echo", "hello"}}
	_, err := rt.RunEphemeral(context.Background(), spec, req)
	if !errors.Is(err, admission.ErrBudgetExceeded) {
		t.Fatalf("want ErrBudgetExceeded, got %v", err)
	}
	if acct.callCount != 1 {
		t.Errorf("accountant called %d times, want 1", acct.callCount)
	}
}

func TestRunEphemeral_FailClosedAccountantError(t *testing.T) {
	t.Setenv(admission.BudgetEnvVar, "65536")

	acct := &fakeAccountant{committedErr: errors.New("probe failed")}
	rt := &Runtime{acct: acct}
	spec := coreruntime.SandboxSpec{Name: "testbox", MemoryMiB: 512}
	req := coreruntime.ExecRequest{Argv: []string{"echo", "hello"}}
	_, err := rt.RunEphemeral(context.Background(), spec, req)
	if !errors.Is(err, admission.ErrCommittedUnknown) {
		t.Fatalf("want ErrCommittedUnknown, got %v", err)
	}
}

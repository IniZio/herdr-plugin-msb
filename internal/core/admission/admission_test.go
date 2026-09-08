package admission

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type stubAccountant struct {
	committed uint32
	err       error
}

func (s *stubAccountant) CommittedMemoryMiB(_ context.Context) (uint32, error) {
	return s.committed, s.err
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name      string
		budget    uint32
		committed uint32
		requested uint32
		wantErr   error
	}{
		{"under budget", 3072, 2048, 512, nil},
		{"exact boundary", 3072, 2048, 1024, nil},
		{"one over boundary", 3072, 2048, 1025, ErrBudgetExceeded},
		{"zero budget", 0, 512, 512, ErrBudgetInvalid},
		{"zero requested", 3072, 512, 0, ErrRequestInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Decide(tc.budget, tc.committed, tc.requested)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Decide(%d,%d,%d) = %v, want %v", tc.budget, tc.committed, tc.requested, err, tc.wantErr)
			}
		})
	}
}

func TestDecide_ErrorMessageContainsFigures(t *testing.T) {
	err := Decide(3072, 2048, 1025)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, needle := range []string{"2048", "1025", "3072"} {
		if !strings.Contains(msg, needle) {
			t.Errorf("error message missing %q: %s", needle, msg)
		}
	}
}

func TestAdmit_NilAccountant(t *testing.T) {
	err := Admit(context.Background(), nil, 512)
	if !errors.Is(err, ErrCommittedUnknown) {
		t.Errorf("nil accountant: want ErrCommittedUnknown, got %v", err)
	}
}

func TestAdmit_AccountantError(t *testing.T) {
	acct := &stubAccountant{committed: 0, err: errors.New("daemon unreachable")}
	err := Admit(context.Background(), acct, 512)
	if !errors.Is(err, ErrCommittedUnknown) {
		t.Errorf("want ErrCommittedUnknown, got %v", err)
	}
	if !strings.Contains(err.Error(), "daemon unreachable") {
		t.Errorf("error missing cause: %s", err.Error())
	}
}

func TestAdmit_OppositeOutcomes(t *testing.T) {
	ctx := context.Background()
	under := &stubAccountant{committed: 2048}
	if err := Admit(ctx, under, 512); err != nil {
		t.Errorf("under budget: want nil, got %v", err)
	}
	over := &stubAccountant{committed: 8000}
	if err := Admit(ctx, over, 512); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("over budget: want ErrBudgetExceeded, got %v", err)
	}
}

func TestBudget(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		prev, had := os.LookupEnv(BudgetEnvVar)
		os.Unsetenv(BudgetEnvVar)
		t.Cleanup(func() {
			if had {
				os.Setenv(BudgetEnvVar, prev)
			} else {
				os.Unsetenv(BudgetEnvVar)
			}
		})
		mib, err := Budget()
		if err != nil || mib != DefaultHostBudgetMiB {
			t.Errorf("unset: got (%d, %v), want (%d, nil)", mib, err, DefaultHostBudgetMiB)
		}
	})
	for _, tc := range []struct {
		name    string
		val     string
		wantMiB uint32
		wantErr error
	}{
		{"empty string", "", DefaultHostBudgetMiB, nil},
		{"valid", "4096", 4096, nil},
		{"zero", "0", 0, ErrBudgetInvalid},
		{"not a number", "notanumber", 0, ErrBudgetInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(BudgetEnvVar, tc.val)
			mib, err := Budget()
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Budget() err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && mib != tc.wantMiB {
				t.Errorf("Budget() mib = %d, want %d", mib, tc.wantMiB)
			}
		})
	}
}

func TestAdmit_BudgetEnvVar(t *testing.T) {
	ctx := context.Background()
	acct := &stubAccountant{committed: 2048}

	t.Setenv(BudgetEnvVar, "8192")
	if err := Admit(ctx, acct, 1500); err != nil {
		t.Errorf("large budget: want nil, got %v", err)
	}

	t.Setenv(BudgetEnvVar, "3072")
	if err := Admit(ctx, acct, 1500); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("small budget: want ErrBudgetExceeded, got %v", err)
	}
}

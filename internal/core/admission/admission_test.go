package admission

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func withMeminfo(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fake meminfo: %v", err)
	}
	withMeminfoPath(t, path)
}

func withMeminfoPath(t *testing.T, path string) {
	t.Helper()
	prev := meminfoPath
	meminfoPath = path
	t.Cleanup(func() { meminfoPath = prev })
}

func meminfoBody(memTotalKiB uint64) string {
	return "MemFree:         1000 kB\nMemTotal:       " +
		strconv.FormatUint(memTotalKiB, 10) + " kB\nSwapTotal:          0 kB\n"
}

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
	withMeminfo(t, meminfoBody(32*1024*1024))
	t.Setenv(BudgetEnvVar, "")
	under := &stubAccountant{committed: 2048}
	if err := Admit(ctx, under, 512); err != nil {
		t.Errorf("under budget: want nil, got %v", err)
	}
	over := &stubAccountant{committed: 7800}
	if err := Admit(ctx, over, 512); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("over budget: want ErrBudgetExceeded, got %v", err)
	}
}

func TestDefaultBudgetMiB_ScalesWithHostRAM(t *testing.T) {
	for _, tc := range []struct {
		name    string
		totKiB  uint64
		wantMiB uint32
	}{
		{"8 GiB host", 8 * 1024 * 1024, 2048},
		{"31200 MiB host", 31949300, 7800},
		{"128 GiB host", 128 * 1024 * 1024, 32768},
		{"2 GiB host", 2 * 1024 * 1024, 512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withMeminfo(t, meminfoBody(tc.totKiB))
			if got := DefaultBudgetMiB(); got != tc.wantMiB {
				t.Errorf("DefaultBudgetMiB() = %d, want %d", got, tc.wantMiB)
			}
		})
	}
}

func TestAdmit_SameCreateRefusedOnSmallHostAdmittedOnLarge(t *testing.T) {
	ctx := context.Background()
	t.Setenv(BudgetEnvVar, "")
	acct := &stubAccountant{committed: 1024}
	const requested = uint32(2048)

	withMeminfo(t, meminfoBody(8*1024*1024))
	if err := Admit(ctx, acct, requested); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("8 GiB host: want ErrBudgetExceeded, got %v", err)
	}

	withMeminfo(t, meminfoBody(64*1024*1024))
	if err := Admit(ctx, acct, requested); err != nil {
		t.Errorf("64 GiB host: want nil, got %v", err)
	}
}

func TestDefaultBudgetMiB_UnreadableMemTotalFallsBackToFloor(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		gone bool
	}{
		{name: "missing file", gone: true},
		{name: "no MemTotal line", body: "MemFree: 1000 kB\n"},
		{name: "unparsable MemTotal", body: "MemTotal:       notanumber kB\n"},
		{name: "truncated MemTotal line", body: "MemTotal:\n"},
		{name: "zero MemTotal", body: meminfoBody(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.gone {
				withMeminfoPath(t, filepath.Join(t.TempDir(), "absent"))
			} else {
				withMeminfo(t, tc.body)
			}
			got := DefaultBudgetMiB()
			if got != FallbackHostBudgetMiB {
				t.Errorf("DefaultBudgetMiB() = %d, want fallback %d", got, FallbackHostBudgetMiB)
			}
			if got >= 8192 {
				t.Errorf("fallback %d must be far below the old 8192 constant", got)
			}
		})
	}
}

func TestAdmit_UnreadableMemTotalRefusesLargeCreate(t *testing.T) {
	ctx := context.Background()
	t.Setenv(BudgetEnvVar, "")
	withMeminfoPath(t, filepath.Join(t.TempDir(), "absent"))
	acct := &stubAccountant{committed: 0}
	if err := Admit(ctx, acct, 4096); !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("unreadable MemTotal, 4096 MiB request: want ErrBudgetExceeded, got %v", err)
	}
	if err := Admit(ctx, acct, 1024); err != nil {
		t.Errorf("unreadable MemTotal, 1024 MiB request: want nil, got %v", err)
	}
}

func TestBudget_EnvOverrideBeatsDerivedBudget(t *testing.T) {
	withMeminfo(t, meminfoBody(8*1024*1024))
	t.Setenv(BudgetEnvVar, "20480")
	mib, err := Budget()
	if err != nil || mib != 20480 {
		t.Errorf("override on an 8 GiB host: got (%d, %v), want (20480, nil)", mib, err)
	}
}

func TestBudget(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		withMeminfo(t, meminfoBody(8*1024*1024))
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
		if err != nil || mib != 2048 {
			t.Errorf("unset on an 8 GiB host: got (%d, %v), want (2048, nil)", mib, err)
		}
	})
	withMeminfo(t, meminfoBody(8*1024*1024))
	for _, tc := range []struct {
		name    string
		val     string
		wantMiB uint32
		wantErr error
	}{
		{"empty string", "", 2048, nil},
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

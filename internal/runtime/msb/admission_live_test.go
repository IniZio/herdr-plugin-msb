package msb

import (
	"errors"
	"fmt"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/admission"
)

func TestLiveCommittedMemoryMiB(t *testing.T) {
	RequireLive(t)
	ctx := LiveContext(t)

	committed, err := New().CommittedMemoryMiB(ctx)
	if err != nil {
		t.Fatalf("CommittedMemoryMiB: %v", err)
	}
	t.Logf("committed memory = %d MiB", committed)

	if committed < 2048 {
		t.Fatalf("committed = %d MiB, want >= 2048 (herdr--eyeball should be running with 2048 MiB)", committed)
	}

	budget := committed + 1024
	t.Setenv(admission.BudgetEnvVar, fmt.Sprintf("%d", budget))
	t.Logf("budget set to %d MiB (committed %d + 1024)", budget, committed)

	if err := admission.Admit(ctx, New(), 512); err != nil {
		t.Errorf("ADMITTED expected: Admit(512 MiB) returned error: %v", err)
	} else {
		t.Logf("ADMITTED: Admit(ctx, New(), 512) = nil (budget=%d committed=%d requested=512)", budget, committed)
	}

	err = admission.Admit(ctx, New(), 2048)
	if err == nil {
		t.Errorf("REFUSED expected: Admit(2048 MiB) returned nil, want ErrBudgetExceeded")
	} else if !errors.Is(err, admission.ErrBudgetExceeded) {
		t.Errorf("REFUSED expected ErrBudgetExceeded, got: %v", err)
	} else {
		t.Logf("REFUSED: Admit(ctx, New(), 2048) = %v", err)
	}
}

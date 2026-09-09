package msb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/IniZio/herdr-plugin-msb/internal/core/admission"
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

func TestMemoryMiBFromRecord(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantMiB    uint32
		wantErr    string
	}{
		{
			name:       "resources.memory_mib present",
			configJSON: `{"resources":{"memory_mib":2048},"memory_mib":0}`,
			wantMiB:    2048,
		},
		{
			name:       "resources absent top-level memory_mib",
			configJSON: `{"memory_mib":512}`,
			wantMiB:    512,
		},
		{
			name:       "resources present but zero top-level set uses top-level",
			configJSON: `{"resources":{"memory_mib":0},"memory_mib":1024}`,
			wantMiB:    1024,
		},
		{
			name:       "malformed JSON",
			configJSON: `{not valid json`,
			wantErr:    "parse config record",
		},
		{
			name:       "empty string",
			configJSON: "",
			wantErr:    "config record is empty",
		},
		{
			name:       "both paths absent or zero",
			configJSON: `{"resources":{"memory_mib":0},"memory_mib":0}`,
			wantErr:    "memory_mib absent or zero",
		},
		{
			name: "realistic full daemon record",
			configJSON: `{
				"id":"abc123",
				"name":"s01--foo",
				"status":"running",
				"resources":{"memory_mib":4096,"max_memory_mib":4096,"vcpus":2},
				"memory_mib":1024,
				"network":{"enabled":true,"policy":{"default_egress":"deny"}},
				"labels":{"herdr.project":"s01","herdr.motive":"test"},
				"created_at":"2026-01-01T00:00:00Z"
			}`,
			wantMiB: 4096,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := memoryMiBFromRecord(tc.configJSON)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (mib=%d)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantMiB {
				t.Fatalf("got %d MiB, want %d MiB", got, tc.wantMiB)
			}
		})
	}
}

type fakeAccountant struct {
	callCount    int
	committed    uint32
	committedErr error
}

func (f *fakeAccountant) CommittedMemoryMiB(_ context.Context) (uint32, error) {
	f.callCount++
	return f.committed, f.committedErr
}

func recJSON(mib uint32) string {
	return fmt.Sprintf(`{"resources":{"memory_mib":%d}}`, mib)
}

func cursorLabel(cursor *string) string {
	if cursor == nil {
		return "<nil>"
	}
	return *cursor
}

func TestSumCommittedMiB_SumsAcrossPages(t *testing.T) {
	var seen []string
	fetch := func(_ context.Context, cursor *string) ([]memRecordRef, *string, error) {
		seen = append(seen, cursorLabel(cursor))
		switch len(seen) {
		case 1:
			next := "p2"
			return []memRecordRef{{name: "a", configJSON: recJSON(1024)}}, &next, nil
		default:
			return []memRecordRef{{name: "b", configJSON: recJSON(2048)}}, nil, nil
		}
	}
	got, err := sumCommittedMiB(context.Background(), fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 3072 {
		t.Fatalf("got %d MiB, want 3072 MiB", got)
	}
	want := []string{"<nil>", "p2"}
	if !slices.Equal(seen, want) {
		t.Fatalf("fetcher received cursors %v, want %v", seen, want)
	}
}

func TestSumCommittedMiB_ThreadsEachNextCursorToTheNextFetch(t *testing.T) {
	pages := []struct {
		wantCursor string
		next       *string
	}{
		{wantCursor: "<nil>", next: ptr("p2")},
		{wantCursor: "p2", next: ptr("p3")},
		{wantCursor: "p3", next: nil},
	}
	var seen []string
	fetch := func(_ context.Context, cursor *string) ([]memRecordRef, *string, error) {
		i := len(seen)
		seen = append(seen, cursorLabel(cursor))
		if i >= len(pages) {
			return nil, nil, fmt.Errorf("fetched page %d beyond the %d staged pages", i+1, len(pages))
		}
		return []memRecordRef{{name: "a", configJSON: recJSON(256)}}, pages[i].next, nil
	}
	got, err := sumCommittedMiB(context.Background(), fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 768 {
		t.Fatalf("got %d MiB, want 768 MiB", got)
	}
	var want []string
	for _, p := range pages {
		want = append(want, p.wantCursor)
	}
	if !slices.Equal(seen, want) {
		t.Fatalf("fetcher received cursors %v, want %v (each page's NextCursor must reach the next fetch)", seen, want)
	}
}

func ptr(s string) *string { return &s }

func TestSumCommittedMiB_RepeatedCursorRefused(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ *string) ([]memRecordRef, *string, error) {
		calls++
		stuck := "same"
		return []memRecordRef{{name: "a", configJSON: recJSON(512)}}, &stuck, nil
	}
	got, err := sumCommittedMiB(context.Background(), fetch)
	if err == nil {
		t.Fatalf("want repeated-cursor error, got nil (mib=%d)", got)
	}
	if !strings.Contains(err.Error(), "repeated list cursor") {
		t.Fatalf("error %q does not mention a repeated list cursor", err.Error())
	}
	if calls != 2 {
		t.Fatalf("fetched %d pages before refusing, want 2", calls)
	}
}

func TestSumCommittedMiB_PageBoundRefused(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ *string) ([]memRecordRef, *string, error) {
		calls++
		next := fmt.Sprintf("cursor-%d", calls)
		return []memRecordRef{{name: "a", configJSON: recJSON(1)}}, &next, nil
	}
	got, err := sumCommittedMiB(context.Background(), fetch)
	if err == nil {
		t.Fatalf("want page-bound error, got nil (mib=%d)", got)
	}
	if !strings.Contains(err.Error(), "exceeded 1000 pages") {
		t.Fatalf("error %q does not report the page bound", err.Error())
	}
	if calls != maxCommittedPages {
		t.Fatalf("fetched %d pages, want the bound %d", calls, maxCommittedPages)
	}
}

func TestSumCommittedMiB_ContextCancelledRefused(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, _ *string) ([]memRecordRef, *string, error) {
		calls++
		return nil, nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := sumCommittedMiB(ctx, fetch)
	if err == nil {
		t.Fatalf("want context error, got nil (mib=%d)", got)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v does not wrap context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("fetched %d pages after cancellation, want 0", calls)
	}
}

func TestSumCommittedMiB_SaturatesInsteadOfWrapping(t *testing.T) {
	fetch := func(_ context.Context, _ *string) ([]memRecordRef, *string, error) {
		return []memRecordRef{
			{name: "a", configJSON: recJSON(math.MaxUint32 - 100)},
			{name: "b", configJSON: recJSON(4096)},
		}, nil, nil
	}
	got, err := sumCommittedMiB(context.Background(), fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != math.MaxUint32 {
		t.Fatalf("got %d MiB, want saturation at %d MiB", got, uint32(math.MaxUint32))
	}
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

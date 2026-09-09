package msb

import (
	"strings"
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

const scratchGitDir = "/home/newman/.claude/jobs/48adef50/tmp/main-repo/.git"
const scratchWTDir = "/home/newman/.claude/jobs/48adef50/tmp/main-repo/.git/worktrees/wt-probe"

func TestLiveNestedMount(t *testing.T) {
	RequireLive(t)

	t.Run("positive_nested_rw_over_ro_parent", func(t *testing.T) {
		ctx := LiveContext(t)
		r := New()
		spec := LiveSpec("nested-mount-pos")
		spec.Mounts = []coreruntime.Mount{
			{HostPath: scratchGitDir, GuestPath: "/probe-git", ReadOnly: true},
			{HostPath: scratchWTDir, GuestPath: "/probe-git/worktrees/wt-probe", ReadOnly: false},
		}
		ref, err := r.CreateAndBoot(ctx, spec)
		if err != nil {
			t.Fatalf("CreateAndBoot: %v", err)
		}
		CleanupSandbox(t, r, ref)

		sb, err := r.connect(ctx, ref)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}

		out, err := sb.Exec(ctx, "sh", []string{"-c",
			"touch /probe-git/worktrees/wt-probe/probe-rw 2>&1; echo EXIT:$?"})
		if err != nil {
			t.Fatalf("Exec nested rw write: %v", err)
		}
		result := strings.TrimSpace(out.Stdout())
		t.Logf("nested rw write: %s", result)
		if !strings.HasSuffix(result, "EXIT:0") {
			t.Errorf("nested rw write FAILED (want EXIT:0): %s", result)
		}

		out2, err := sb.Exec(ctx, "sh", []string{"-c",
			"touch /probe-git/probe-ro-parent 2>&1; echo EXIT:$?"})
		if err != nil {
			t.Fatalf("Exec ro parent write: %v", err)
		}
		result2 := strings.TrimSpace(out2.Stdout())
		t.Logf("ro parent write: %s", result2)
		if strings.HasSuffix(result2, "EXIT:0") {
			t.Errorf("ro parent write SUCCEEDED (want failure — ro flag not honoured): %s", result2)
		}

		if err := sb.Detach(ctx); err != nil {
			t.Fatalf("Detach: %v", err)
		}
	})

	t.Run("negative_no_nested_rw_mount", func(t *testing.T) {
		ctx := LiveContext(t)
		r := New()
		spec := LiveSpec("nested-mount-neg")
		spec.Mounts = []coreruntime.Mount{
			{HostPath: scratchGitDir, GuestPath: "/probe-git", ReadOnly: true},
		}
		ref, err := r.CreateAndBoot(ctx, spec)
		if err != nil {
			t.Fatalf("CreateAndBoot: %v", err)
		}
		CleanupSandbox(t, r, ref)

		sb, err := r.connect(ctx, ref)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}

		out, err := sb.Exec(ctx, "sh", []string{"-c",
			"touch /probe-git/worktrees/wt-probe/probe-rw 2>&1; echo EXIT:$?"})
		if err != nil {
			t.Fatalf("Exec nested path write (no nested mount): %v", err)
		}
		result := strings.TrimSpace(out.Stdout())
		t.Logf("write under ro-only (no nested rw): %s", result)
		if strings.HasSuffix(result, "EXIT:0") {
			t.Errorf("write SUCCEEDED (want failure — parent is ro, no nested rw mount): %s", result)
		}

		if err := sb.Detach(ctx); err != nil {
			t.Fatalf("Detach: %v", err)
		}
	})
}

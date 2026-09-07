package runtime

import "context"

type Runtime interface {
	Create(ctx context.Context, spec SandboxSpec) (SandboxRef, error)
	CreateAndBoot(ctx context.Context, spec SandboxSpec) (SandboxRef, error)
	List(ctx context.Context) ([]SandboxRef, error)
	Start(ctx context.Context, ref SandboxRef) (SandboxRef, error)
	Stop(ctx context.Context, ref SandboxRef) (SandboxRef, error)
	Pause(ctx context.Context, ref SandboxRef) (SandboxRef, error)
	Resume(ctx context.Context, ref SandboxRef) (SandboxRef, error)
	Remove(ctx context.Context, ref SandboxRef) error
	Exec(ctx context.Context, ref SandboxRef, req ExecRequest) (ExecResult, error)
	RunEphemeral(ctx context.Context, spec SandboxSpec, req ExecRequest) (ExecResult, error)
}

package msb

import (
	"context"
	"fmt"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

func (r *Runtime) List(ctx context.Context) ([]coreruntime.SandboxRef, error) {
	refs := []coreruntime.SandboxRef{}
	page, err := msbsdk.ListSandboxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("msb: list: %w", err)
	}
	for _, h := range page.Sandboxes {
		refs = append(refs, refFromHandle(h))
	}
	for page.NextCursor != nil {
		page, err = msbsdk.ListSandboxesWith(ctx, msbsdk.WithListCursor(*page.NextCursor))
		if err != nil {
			return nil, fmt.Errorf("msb: list (cursor): %w", err)
		}
		for _, h := range page.Sandboxes {
			refs = append(refs, refFromHandle(h))
		}
	}
	return refs, nil
}

func (r *Runtime) Start(ctx context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	h, err := r.handle(ctx, ref)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	sb, err := h.StartDetached(ctx)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: start %q: %w", h.Name(), err)
	}
	if err := sb.Detach(ctx); err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: detach %q after start: %w", h.Name(), err)
	}
	fresh, err := h.Refresh(ctx)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: refresh %q after start: %w", h.Name(), err)
	}
	return refFromHandle(fresh), nil
}

func (r *Runtime) Stop(ctx context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	h, err := r.handle(ctx, ref)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	if err := h.Stop(ctx); err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: stop %q: %w", h.Name(), err)
	}
	fresh, err := h.Refresh(ctx)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: refresh %q after stop: %w", h.Name(), err)
	}
	return refFromHandle(fresh), nil
}

func (r *Runtime) Pause(_ context.Context, _ coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return coreruntime.SandboxRef{}, fmt.Errorf("msb: pause: %w", ErrUnsupported)
}

func (r *Runtime) Resume(_ context.Context, _ coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return coreruntime.SandboxRef{}, fmt.Errorf("msb: resume: %w", ErrUnsupported)
}

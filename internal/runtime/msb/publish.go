//go:build linux

package msb

import (
	"context"
	"fmt"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

var _ coreruntime.Publisher = (*Runtime)(nil)

func (r *Runtime) Publish(ctx context.Context, spec coreruntime.SandboxSpec, ports []uint16) (coreruntime.SandboxRef, error) {
	name := spec.Name
	if spec.Project != "" {
		name = SDKName(spec.Project, spec.Name)
	}
	if err := coreruntime.ValidatePorts(ports); err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: publish %q: %w", name, err)
	}
	merged := coreruntime.MergePorts(spec.Ports, ports)
	specWithPorts := spec
	specWithPorts.Ports = merged

	ref := coreruntime.SandboxRef{Project: spec.Project, Name: spec.Name}
	if h, err := r.handle(ctx, ref); err == nil {
		_ = h.Kill(ctx)
		if rerr := h.Remove(ctx); rerr != nil {
			return coreruntime.SandboxRef{}, fmt.Errorf("msb: publish %q: remove existing: %w", name, rerr)
		}
	}

	result, err := r.CreateAndBoot(ctx, specWithPorts)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: publish %q: %w", name, err)
	}
	return result, nil
}

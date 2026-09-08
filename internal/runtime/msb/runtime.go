package msb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/IniZio/herdr-plugin-msb/internal/core/admission"
	"github.com/IniZio/herdr-plugin-msb/internal/core/netprofile"
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

var ErrUnsupported = errors.New("msb: operation not supported by microsandbox v0.6.17")

const (
	LabelProject = "herdr.project"
	LabelMotive  = "herdr.motive"
	nameSep      = "--"
)

type Runtime struct{}

var _ coreruntime.Runtime = (*Runtime)(nil)

func New() *Runtime { return &Runtime{} }

func SDKName(project, name string) string {
	if project == "" {
		return name
	}
	return project + nameSep + name
}

func statusFrom(s msbsdk.SandboxStatus) coreruntime.SandboxStatus {
	switch s {
	case msbsdk.SandboxStatusRunning, msbsdk.SandboxStatusStarting, msbsdk.SandboxStatusDraining:
		return coreruntime.SandboxStatusRunning
	case msbsdk.SandboxStatusPaused:
		return coreruntime.SandboxStatusPaused
	case msbsdk.SandboxStatusCreated:
		return coreruntime.SandboxStatusCreated
	default:
		return coreruntime.SandboxStatusStopped
	}
}

func refFromHandle(h *msbsdk.SandboxHandle) coreruntime.SandboxRef {
	ref := coreruntime.SandboxRef{
		ID:     h.ID(),
		Name:   h.Name(),
		Status: statusFrom(h.Status()),
	}
	if cfg, err := h.Config(); err == nil && cfg != nil {
		ref.Project = cfg.Labels[LabelProject]
	}
	if ref.Project == "" {
		if project, name, ok := strings.Cut(h.Name(), nameSep); ok {
			ref.Project, ref.Name = project, name
		}
	} else {
		ref.Name = strings.TrimPrefix(h.Name(), ref.Project+nameSep)
	}
	return ref
}

func SandboxOptions(spec coreruntime.SandboxSpec) []msbsdk.SandboxOption {
	opts := []msbsdk.SandboxOption{msbsdk.WithQuietLogs()}
	if spec.ImageRef != "" {
		opts = append(opts, msbsdk.WithImage(spec.ImageRef))
	}
	if spec.VCPUs > 0 {
		opts = append(opts, msbsdk.WithCPUs(uint8(spec.VCPUs)), msbsdk.WithMaxCPUs(uint8(spec.VCPUs)))
	}
	if spec.MemoryMiB > 0 {
		opts = append(opts, msbsdk.WithMemory(spec.MemoryMiB), msbsdk.WithMaxMemory(spec.MemoryMiB))
	}
	labels := map[string]string{}
	if spec.Project != "" {
		labels[LabelProject] = spec.Project
	}
	if spec.Motive != "" {
		labels[LabelMotive] = spec.Motive
	}
	if len(labels) > 0 {
		opts = append(opts, msbsdk.WithLabels(labels))
	}
	if len(spec.Mounts) > 0 {
		mounts := make(map[string]msbsdk.MountConfig, len(spec.Mounts))
		for _, m := range spec.Mounts {
			mounts[m.GuestPath] = msbsdk.Mount.Bind(m.HostPath, msbsdk.MountOptions{
				Readonly:           m.ReadOnly,
				Noexec:             m.Noexec,
				Nosuid:             m.Nosuid,
				Nodev:              m.Nodev,
				StatVirtualization: msbsdk.StatVirtualizationRelaxed,
				HostPermissions:    msbsdk.HostPermissionsMirror,
			})
		}
		opts = append(opts, msbsdk.WithMounts(mounts))
	}
	if len(spec.Ports) > 0 {
		opts = append(opts, msbsdk.WithPorts(coreruntime.SamePortMap(spec.Ports)))
	}
	opts = append(opts, msbsdk.WithNetwork(networkConfig(spec.NetRules)))
	if spec.RemoveOnExit {
		opts = append(opts, msbsdk.WithEphemeral(true))
	}
	return opts
}

func networkConfig(rules []coreruntime.NetRule) *msbsdk.NetworkConfig {
	if len(rules) == 0 {
		rules = netprofile.Shipped()
	}
	net := &msbsdk.NetworkConfig{DefaultEgress: msbsdk.PolicyActionDeny}
	net.Rules = append(net.Rules, msbsdk.Rule.AllowDNS())
	for _, r := range rules {
		action := msbsdk.PolicyActionAllow
		if r.Action == coreruntime.NetDeny {
			action = msbsdk.PolicyActionDeny
		}
		rule := msbsdk.PolicyRule{
			Action:      action,
			Direction:   msbsdk.PolicyDirectionEgress,
			Destination: r.Host,
			Protocol:    msbsdk.PolicyProtocolTCP,
		}
		if r.Port > 0 {
			rule.Port = fmt.Sprintf("%d", r.Port)
		}
		net.Rules = append(net.Rules, rule)
	}
	return net
}

func (r *Runtime) handle(ctx context.Context, ref coreruntime.SandboxRef) (*msbsdk.SandboxHandle, error) {
	name := ref.Name
	if ref.Project != "" {
		name = SDKName(ref.Project, ref.Name)
	}
	if name == "" {
		return nil, fmt.Errorf("msb: sandbox ref has no name")
	}
	h, err := msbsdk.GetSandbox(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("msb: get sandbox %q: %w", name, err)
	}
	return h, nil
}

func (r *Runtime) connect(ctx context.Context, ref coreruntime.SandboxRef) (*msbsdk.Sandbox, error) {
	h, err := r.handle(ctx, ref)
	if err != nil {
		return nil, err
	}
	sb, err := h.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("msb: connect %q: %w", h.Name(), err)
	}
	return sb, nil
}

func (r *Runtime) Create(ctx context.Context, spec coreruntime.SandboxSpec) (coreruntime.SandboxRef, error) {
	ref, err := r.CreateAndBoot(ctx, spec)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	h, err := r.handle(ctx, ref)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	if err := h.Stop(ctx); err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: stop %q after create: %w", h.Name(), err)
	}
	fresh, err := h.Refresh(ctx)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: refresh %q after create: %w", h.Name(), err)
	}
	return refFromHandle(fresh), nil
}

func (r *Runtime) CreateAndBoot(ctx context.Context, spec coreruntime.SandboxSpec) (coreruntime.SandboxRef, error) {
	name := SDKName(spec.Project, spec.Name)
	if name == "" {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: spec has no name")
	}
	if err := admission.Admit(ctx, r, spec.MemoryMiB); err != nil {
		return coreruntime.SandboxRef{}, err
	}
	opts := append(SandboxOptions(spec), msbsdk.WithDetached())
	sb, err := msbsdk.CreateSandbox(ctx, name, opts...)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: create sandbox %q: %w", name, err)
	}
	if err := sb.Detach(ctx); err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: detach %q: %w", name, err)
	}
	h, err := msbsdk.GetSandbox(ctx, name)
	if err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("msb: get sandbox %q after create: %w", name, err)
	}
	if perr := assertNetworkPolicy(ctx, h); perr != nil {
		return coreruntime.SandboxRef{}, errors.Join(perr, teardownHandle(ctx, h))
	}
	return refFromHandle(h), nil
}

func (r *Runtime) Remove(ctx context.Context, ref coreruntime.SandboxRef) error {
	h, err := r.handle(ctx, ref)
	if err != nil {
		return err
	}
	if err := h.Remove(ctx); err != nil {
		return fmt.Errorf("msb: remove %q: %w", h.Name(), err)
	}
	return nil
}

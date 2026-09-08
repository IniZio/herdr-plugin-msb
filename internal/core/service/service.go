package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/IniZio/herdr-plugin-msb/internal/core/credmount"
	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

const (
	DefaultProject         = "herdr"
	DefaultGuestWorktree   = "/workspace"
	DefaultGuestCredential = "/root/.claude/.credentials.json"
	DefaultGuestCredDir    = "/root/.claude"
	DefaultMemoryMiB       = 1024
	MaxMemoryMiB           = 2048
)

var (
	ErrNameRequired   = errors.New("service: sandbox name is required")
	ErrImageRequired  = errors.New("service: image ref is required")
	ErrMemoryTooLarge = errors.New("service: memory exceeds the 2048 MiB cap")
	ErrNotFound       = errors.New("service: sandbox not found")
)

type Service struct {
	RT      coreruntime.Runtime
	Project string
}

func New(rt coreruntime.Runtime, project string) *Service {
	if project == "" {
		project = DefaultProject
	}
	return &Service{RT: rt, Project: project}
}

type CreateOptions struct {
	Name            string
	ImageRef        string
	VCPUs           uint32
	MemoryMiB       uint32
	Motive          string
	Worktree        string
	GuestWorktree   string
	Credential      bool
	CredentialPath  string
	GuestCredential string
	Ports           []uint16
	Boot            bool
}

func (s *Service) Spec(opts CreateOptions) (coreruntime.SandboxSpec, error) {
	if opts.Name == "" {
		return coreruntime.SandboxSpec{}, ErrNameRequired
	}
	if opts.ImageRef == "" {
		return coreruntime.SandboxSpec{}, ErrImageRequired
	}
	mem := opts.MemoryMiB
	if mem == 0 {
		mem = DefaultMemoryMiB
	}
	if mem > MaxMemoryMiB {
		return coreruntime.SandboxSpec{}, ErrMemoryTooLarge
	}
	spec := coreruntime.SandboxSpec{
		Project:   s.Project,
		Name:      opts.Name,
		ImageRef:  opts.ImageRef,
		VCPUs:     opts.VCPUs,
		MemoryMiB: mem,
		Motive:    opts.Motive,
		Ports:     append([]uint16(nil), opts.Ports...),
	}
	if opts.Worktree != "" {
		gw := opts.GuestWorktree
		if gw == "" {
			gw = DefaultGuestWorktree
		}
		m, err := credmount.DirMount(opts.Worktree, gw, false)
		if err != nil {
			return coreruntime.SandboxSpec{}, err
		}
		spec.Mounts = append(spec.Mounts, m)
	}
	if opts.Credential {
		credDir := opts.CredentialPath
		if credDir == "" {
			credDir = credmount.DefaultStoreDir()
		}
		gc := opts.GuestCredential
		if gc == "" {
			gc = DefaultGuestCredDir
		}
		m, err := credmount.DirMount(credDir, gc, false)
		if err != nil {
			return coreruntime.SandboxSpec{}, err
		}
		spec.Mounts = append(spec.Mounts, m)
	}
	// NetRules left nil; single application point is runtime.go:105.
	return spec, nil
}

func (s *Service) Create(ctx context.Context, opts CreateOptions) (coreruntime.SandboxRef, error) {
	spec, err := s.Spec(opts)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	if opts.Boot {
		return s.RT.CreateAndBoot(ctx, spec)
	}
	return s.RT.Create(ctx, spec)
}

func (s *Service) List(ctx context.Context) ([]coreruntime.SandboxRef, error) {
	return s.RT.List(ctx)
}

func (s *Service) Resolve(ctx context.Context, name string) (coreruntime.SandboxRef, error) {
	refs, err := s.RT.List(ctx)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	for _, r := range refs {
		if r.Name == name && (r.Project == "" || r.Project == s.Project) {
			return r, nil
		}
	}
	return coreruntime.SandboxRef{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

func (s *Service) Start(ctx context.Context, name string) (coreruntime.SandboxRef, error) {
	ref, err := s.Resolve(ctx, name)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	return s.RT.Start(ctx, ref)
}

func (s *Service) Stop(ctx context.Context, name string) (coreruntime.SandboxRef, error) {
	ref, err := s.Resolve(ctx, name)
	if err != nil {
		return coreruntime.SandboxRef{}, err
	}
	return s.RT.Stop(ctx, ref)
}

func (s *Service) Remove(ctx context.Context, name string) error {
	ref, err := s.Resolve(ctx, name)
	if err != nil {
		return err
	}
	return s.RT.Remove(ctx, ref)
}

func (s *Service) Exec(ctx context.Context, name string, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	ref, err := s.Resolve(ctx, name)
	if err != nil {
		return coreruntime.ExecResult{}, err
	}
	return s.RT.Exec(ctx, ref, req)
}

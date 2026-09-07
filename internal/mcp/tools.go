package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

// Tool is an advertised MCP tool with its JSON schema and handler.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(ctx context.Context, args json.RawMessage) (any, error)
}

var bannedParams = []string{"rootfs_path", "memory_mib", "nested_virt"}

func checkBanned(args json.RawMessage) error {
	if len(args) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(args, &m); err != nil {
		return nil
	}
	for _, k := range bannedParams {
		if _, ok := m[k]; ok {
			return fmt.Errorf("parameter %q is not accepted by this server (D-9: substrate detail excluded from MCP surface)", k)
		}
	}
	return nil
}

func parseRef(raw json.RawMessage) (coreruntime.SandboxRef, error) {
	var p struct {
		Name    string `json:"name"`
		Project string `json:"project"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return coreruntime.SandboxRef{}, fmt.Errorf("parse args: %w", err)
	}
	if p.Name == "" {
		return coreruntime.SandboxRef{}, fmt.Errorf("name is required")
	}
	return coreruntime.SandboxRef{Name: p.Name, Project: p.Project}, nil
}

func refSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Sandbox name"},
			"project": map[string]any{"type": "string", "description": "Project label"},
		},
		"required": []string{"name"},
	}
}

func buildTools(rt coreruntime.Runtime) []Tool {
	return []Tool{
		toolCreate(rt),
		toolList(rt),
		toolStart(rt),
		toolStop(rt),
		toolRemove(rt),
		toolExec(rt),
	}
}

func toolCreate(rt coreruntime.Runtime) Tool {
	return Tool{
		Name:        "sandbox_create",
		Description: "Create and boot a sandbox.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Sandbox name (required)"},
				"project":   map[string]any{"type": "string", "description": "Project label"},
				"image_ref": map[string]any{"type": "string", "description": "OCI image (default: alpine)"},
				"vcpus":     map[string]any{"type": "integer", "description": "vCPU count (default: 1)"},
			},
			"required": []string{"name"},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if err := checkBanned(raw); err != nil {
				return nil, err
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			spec := coreruntime.SandboxSpec{VCPUs: 1}
			if v, ok := m["name"]; ok {
				_ = json.Unmarshal(v, &spec.Name)
			}
			if spec.Name == "" {
				return nil, fmt.Errorf("name is required")
			}
			if v, ok := m["project"]; ok {
				_ = json.Unmarshal(v, &spec.Project)
			}
			if v, ok := m["image_ref"]; ok {
				_ = json.Unmarshal(v, &spec.ImageRef)
			}
			if v, ok := m["vcpus"]; ok {
				_ = json.Unmarshal(v, &spec.VCPUs)
			}
			ref, err := rt.CreateAndBoot(ctx, spec)
			if err != nil {
				return nil, err
			}
			return ref, nil
		},
	}
}

func toolList(rt coreruntime.Runtime) Tool {
	return Tool{
		Name:        "sandbox_list",
		Description: "List sandboxes.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if err := checkBanned(raw); err != nil {
				return nil, err
			}
			refs, err := rt.List(ctx)
			if err != nil {
				return nil, err
			}
			return refs, nil
		},
	}
}

func toolStart(rt coreruntime.Runtime) Tool {
	return Tool{
		Name:        "sandbox_start",
		Description: "Start a stopped sandbox.",
		InputSchema: refSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if err := checkBanned(raw); err != nil {
				return nil, err
			}
			ref, err := parseRef(raw)
			if err != nil {
				return nil, err
			}
			updated, err := rt.Start(ctx, ref)
			if err != nil {
				return nil, err
			}
			return updated, nil
		},
	}
}

func toolStop(rt coreruntime.Runtime) Tool {
	return Tool{
		Name:        "sandbox_stop",
		Description: "Stop a running sandbox.",
		InputSchema: refSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if err := checkBanned(raw); err != nil {
				return nil, err
			}
			ref, err := parseRef(raw)
			if err != nil {
				return nil, err
			}
			updated, err := rt.Stop(ctx, ref)
			if err != nil {
				return nil, err
			}
			return updated, nil
		},
	}
}

func toolRemove(rt coreruntime.Runtime) Tool {
	return Tool{
		Name:        "sandbox_remove",
		Description: "Remove a sandbox permanently.",
		InputSchema: refSchema(),
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if err := checkBanned(raw); err != nil {
				return nil, err
			}
			ref, err := parseRef(raw)
			if err != nil {
				return nil, err
			}
			if err := rt.Remove(ctx, ref); err != nil {
				return nil, err
			}
			return map[string]string{"status": "removed"}, nil
		},
	}
}

func toolExec(rt coreruntime.Runtime) Tool {
	return Tool{
		Name:        "sandbox_exec",
		Description: "Execute a command in a running sandbox.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string"},
				"project": map[string]any{"type": "string"},
				"argv":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"env":     map[string]any{"type": "object"},
				"cwd":     map[string]any{"type": "string"},
				"stdin":   map[string]any{"type": "string"},
			},
			"required": []string{"name", "argv"},
		},
		Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if err := checkBanned(raw); err != nil {
				return nil, err
			}
			var p struct {
				Name    string            `json:"name"`
				Project string            `json:"project"`
				Argv    []string          `json:"argv"`
				Env     map[string]string `json:"env"`
				Cwd     string            `json:"cwd"`
				Stdin   string            `json:"stdin"`
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if p.Name == "" {
				return nil, fmt.Errorf("name is required")
			}
			if len(p.Argv) == 0 {
				return nil, fmt.Errorf("argv is required")
			}
			ref := coreruntime.SandboxRef{Name: p.Name, Project: p.Project}
			var stdout, stderr bytes.Buffer
			req := coreruntime.ExecRequest{
				Argv:   p.Argv,
				Env:    p.Env,
				Cwd:    p.Cwd,
				Stdin:  p.Stdin,
				Stdout: &stdout,
				Stderr: &stderr,
			}
			result, err := rt.Exec(ctx, ref, req)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"exit_code": result.ExitCode,
				"stdout":    stdout.String(),
				"stderr":    stderr.String(),
			}, nil
		},
	}
}

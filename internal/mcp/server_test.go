package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	"github.com/IniZio/herdr-plugin-msb/internal/mcp"
)

type mockRuntime struct {
	created []coreruntime.SandboxSpec
	listed  []coreruntime.SandboxRef
	started coreruntime.SandboxRef
	stopped coreruntime.SandboxRef
	removed coreruntime.SandboxRef
	execReq coreruntime.ExecRequest
}

func (m *mockRuntime) Create(_ context.Context, spec coreruntime.SandboxSpec) (coreruntime.SandboxRef, error) {
	m.created = append(m.created, spec)
	return coreruntime.SandboxRef{Name: spec.Name, Project: spec.Project, Status: coreruntime.SandboxStatusCreated}, nil
}

func (m *mockRuntime) CreateAndBoot(_ context.Context, spec coreruntime.SandboxSpec) (coreruntime.SandboxRef, error) {
	m.created = append(m.created, spec)
	return coreruntime.SandboxRef{Name: spec.Name, Project: spec.Project, Status: coreruntime.SandboxStatusRunning}, nil
}

func (m *mockRuntime) List(_ context.Context) ([]coreruntime.SandboxRef, error) {
	return m.listed, nil
}

func (m *mockRuntime) Start(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	m.started = ref
	return coreruntime.SandboxRef{Name: ref.Name, Status: coreruntime.SandboxStatusRunning}, nil
}

func (m *mockRuntime) Stop(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	m.stopped = ref
	return coreruntime.SandboxRef{Name: ref.Name, Status: coreruntime.SandboxStatusStopped}, nil
}

func (m *mockRuntime) Pause(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return ref, nil
}

func (m *mockRuntime) Resume(_ context.Context, ref coreruntime.SandboxRef) (coreruntime.SandboxRef, error) {
	return ref, nil
}

func (m *mockRuntime) Remove(_ context.Context, ref coreruntime.SandboxRef) error {
	m.removed = ref
	return nil
}

func (m *mockRuntime) Exec(_ context.Context, _ coreruntime.SandboxRef, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	m.execReq = req
	if req.Stdout != nil {
		_, _ = io.WriteString(req.Stdout, "mock-out\n")
	}
	return coreruntime.ExecResult{ExitCode: 0}, nil
}

func (m *mockRuntime) RunEphemeral(_ context.Context, _ coreruntime.SandboxSpec, req coreruntime.ExecRequest) (coreruntime.ExecResult, error) {
	return coreruntime.ExecResult{ExitCode: 0}, nil
}

func rpcCall(t *testing.T, srv *mcp.Server, method string, params any) map[string]any {
	t.Helper()
	id := json.RawMessage(`1`)
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(string(raw)+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw=%s)", err, out.String())
	}
	return resp
}

func TestToolsList(t *testing.T) {
	srv := mcp.NewServer(&mockRuntime{})
	resp := rpcCall(t, srv, "tools/list", nil)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got: %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got: %v", result)
	}
	want := []string{"sandbox_create", "sandbox_list", "sandbox_start", "sandbox_stop", "sandbox_remove", "sandbox_exec"}
	if len(tools) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(tools), len(want))
	}
	for i, w := range want {
		got := tools[i].(map[string]any)["name"].(string)
		if got != w {
			t.Errorf("tools[%d].name = %q, want %q", i, got, w)
		}
	}
}

func TestToolsListDoesNotAdvertisePauseOrResume(t *testing.T) {
	srv := mcp.NewServer(&mockRuntime{})
	resp := rpcCall(t, srv, "tools/list", nil)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	for _, tool := range tools {
		name := tool.(map[string]any)["name"].(string)
		if name == "sandbox_pause" || name == "sandbox_resume" {
			t.Errorf("tool %q must not be advertised (D-14)", name)
		}
	}
}

func TestToolsListDoesNotAdvertiseBannedParams(t *testing.T) {
	srv := mcp.NewServer(&mockRuntime{})
	resp := rpcCall(t, srv, "tools/list", nil)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	banned := []string{"rootfs_path", "memory_mib", "nested_virt"}
	for _, tool := range tools {
		tm := tool.(map[string]any)
		schema, _ := tm["inputSchema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		for _, b := range banned {
			if _, ok := props[b]; ok {
				t.Errorf("tool %q advertises banned param %q (D-9)", tm["name"], b)
			}
		}
	}
}

func TestBannedParamExplicitError(t *testing.T) {
	rt := &mockRuntime{}
	srv := mcp.NewServer(rt)
	for _, banned := range []string{"rootfs_path", "memory_mib", "nested_virt"} {
		args, _ := json.Marshal(map[string]any{"name": "x", banned: "val"})
		params, _ := json.Marshal(map[string]any{"name": "sandbox_create", "arguments": json.RawMessage(args)})
		req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(params) + "}\n"
		var out bytes.Buffer
		_ = srv.Serve(context.Background(), strings.NewReader(req), &out)
		var resp map[string]any
		_ = json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp)
		result, _ := resp["result"].(map[string]any)
		if result == nil {
			t.Fatalf("banned param %q: no result in response", banned)
		}
		if result["isError"] != true {
			t.Errorf("banned param %q: expected isError=true, got result=%v", banned, result)
		}
		content, _ := result["content"].([]any)
		if len(content) == 0 {
			t.Errorf("banned param %q: no error content", banned)
			continue
		}
		text, _ := content[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, banned) {
			t.Errorf("banned param %q: error text %q does not name the param", banned, text)
		}
	}
}

func TestSandboxExecMock(t *testing.T) {
	rt := &mockRuntime{}
	srv := mcp.NewServer(rt)
	args, _ := json.Marshal(map[string]any{
		"name":    "mybox",
		"project": "proj",
		"argv":    []string{"echo", "hi"},
	})
	params, _ := json.Marshal(map[string]any{"name": "sandbox_exec", "arguments": json.RawMessage(args)})
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(params) + "}\n"
	var out bytes.Buffer
	_ = srv.Serve(context.Background(), strings.NewReader(req), &out)
	var resp map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp)
	result, _ := resp["result"].(map[string]any)
	if result == nil || result["isError"] == true {
		t.Fatalf("expected success result, got: %v", resp)
	}
	if rt.execReq.Argv[0] != "echo" {
		t.Errorf("execReq.Argv[0] = %q, want echo", rt.execReq.Argv[0])
	}
}

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
	"github.com/IniZio/herdr-plugin-msb/internal/mcp"
	msbruntime "github.com/IniZio/herdr-plugin-msb/internal/runtime/msb"
)

const liveEnv = "HERDR_MSB_LIVE"

func requireLiveMCP(t *testing.T) {
	t.Helper()
	if os.Getenv(liveEnv) != "1" {
		t.Skipf("live test: set %s=1 to run against a real microsandbox VM", liveEnv)
	}
}

func liveCtxMCP(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

type mcpClient struct {
	srv *mcp.Server
	seq int
}

func (c *mcpClient) call(ctx context.Context, t *testing.T, method string, params any) map[string]any {
	t.Helper()
	c.seq++
	id := json.RawMessage(fmt.Sprintf("%d", c.seq))
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := c.srv.Serve(ctx, strings.NewReader(string(raw)+"\n"), &out); err != nil {
		t.Fatalf("Serve(%s): %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("unmarshal response for %s: %v (raw=%s)", method, err, out.String())
	}
	return resp
}

func (c *mcpClient) toolCall(ctx context.Context, t *testing.T, tool string, args any) map[string]any {
	t.Helper()
	rawArgs, _ := json.Marshal(args)
	params := map[string]any{"name": tool, "arguments": json.RawMessage(rawArgs)}
	resp := c.call(ctx, t, "tools/call", params)
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tool %s: no result (resp=%v)", tool, resp)
	}
	return result
}

func mustSuccess(t *testing.T, tool string, result map[string]any) string {
	t.Helper()
	if result["isError"] == true {
		content, _ := result["content"].([]any)
		text := ""
		if len(content) > 0 {
			text, _ = content[0].(map[string]any)["text"].(string)
		}
		t.Fatalf("tool %s returned error: %s", tool, text)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}

func TestLiveMCPAllTools(t *testing.T) {
	requireLiveMCP(t)
	ctx := liveCtxMCP(t)
	rt := msbruntime.New()
	srv := mcp.NewServer(rt)
	c := &mcpClient{srv: srv}

	_ = c.call(ctx, t, "initialize", map[string]any{
		"protocolVersion": mcp.ProtocolVersion,
		"clientInfo":      map[string]any{"name": "test", "version": "0.0.1"},
	})

	listResp := c.call(ctx, t, "tools/list", nil)
	toolsResult, _ := listResp["result"].(map[string]any)
	tools, _ := toolsResult["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("tools/list returned empty tool set")
	}
	t.Logf("advertised tools (%d):", len(tools))
	for _, tool := range tools {
		tm := tool.(map[string]any)
		t.Logf("  %s: %s", tm["name"], tm["description"])
	}

	const sandboxName = "s09-mcp"
	const project = "s09"

	liveRT := msbruntime.New()
	existing, _ := liveRT.List(ctx)
	for _, ref := range existing {
		if ref.Name == sandboxName && ref.Project == project {
			_ = liveRT.Remove(ctx, ref)
		}
	}

	t.Log("create via MCP")
	createResult := c.toolCall(ctx, t, "sandbox_create", map[string]any{
		"name":      sandboxName,
		"project":   project,
		"image_ref": "alpine",
		"vcpus":     1,
	})
	createText := mustSuccess(t, "sandbox_create", createResult)
	t.Logf("create result: %s", createText)

	var createdRef coreruntime.SandboxRef
	if err := json.Unmarshal([]byte(createText), &createdRef); err != nil {
		t.Fatalf("parse create result: %v", err)
	}
	if createdRef.Name == "" {
		t.Fatal("created ref has no name")
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cleanRT := msbruntime.New()
		refs, _ := cleanRT.List(cleanCtx)
		for _, ref := range refs {
			if ref.Name == sandboxName && ref.Project == project {
				_ = cleanRT.Remove(cleanCtx, ref)
			}
		}
	})

	t.Log("list via MCP")
	listBoxResult := c.toolCall(ctx, t, "sandbox_list", map[string]any{})
	listText := mustSuccess(t, "sandbox_list", listBoxResult)
	if !strings.Contains(listText, sandboxName) {
		t.Errorf("sandbox_list result does not contain %q: %s", sandboxName, listText)
	}

	t.Log("exec via MCP")
	execResult := c.toolCall(ctx, t, "sandbox_exec", map[string]any{
		"name":    sandboxName,
		"project": project,
		"argv":    []string{"sh", "-c", "echo s09-ok"},
	})
	execText := mustSuccess(t, "sandbox_exec", execResult)
	t.Logf("exec result: %s", execText)
	var execOut map[string]any
	if err := json.Unmarshal([]byte(execText), &execOut); err != nil {
		t.Fatalf("parse exec result: %v", err)
	}
	stdout, _ := execOut["stdout"].(string)
	if !strings.Contains(stdout, "s09-ok") {
		t.Errorf("exec stdout = %q, want to contain s09-ok", stdout)
	}

	t.Log("stop via MCP")
	stopResult := c.toolCall(ctx, t, "sandbox_stop", map[string]any{
		"name":    sandboxName,
		"project": project,
	})
	mustSuccess(t, "sandbox_stop", stopResult)

	t.Log("start via MCP")
	startResult := c.toolCall(ctx, t, "sandbox_start", map[string]any{
		"name":    sandboxName,
		"project": project,
	})
	mustSuccess(t, "sandbox_start", startResult)

	t.Log("stop before remove via MCP")
	stopResult2 := c.toolCall(ctx, t, "sandbox_stop", map[string]any{
		"name":    sandboxName,
		"project": project,
	})
	mustSuccess(t, "sandbox_stop (pre-remove)", stopResult2)

	t.Log("remove via MCP")
	removeResult := c.toolCall(ctx, t, "sandbox_remove", map[string]any{
		"name":    sandboxName,
		"project": project,
	})
	removeText := mustSuccess(t, "sandbox_remove", removeResult)
	if !strings.Contains(removeText, "removed") {
		t.Errorf("remove result = %q, want to contain removed", removeText)
	}

	t.Log("verify removed")
	finalRefs, _ := liveRT.List(ctx)
	for _, ref := range finalRefs {
		if ref.Name == sandboxName && ref.Project == project {
			t.Errorf("sandbox %s/%s still exists after remove", project, sandboxName)
		}
	}
}

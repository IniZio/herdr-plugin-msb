//go:build linux

package msb

import (
	"context"
	"encoding/json"
	"fmt"

	msbsdk "github.com/superradcompany/microsandbox/sdk/go"
)

type netPolicyRecord struct {
	Network *struct {
		Enabled bool `json:"enabled"`
		Policy  *struct {
			DefaultEgress string `json:"default_egress"`
		} `json:"policy"`
	} `json:"network"`
}

func assertNetworkPolicy(ctx context.Context, h *msbsdk.SandboxHandle) error {
	name := h.Name()
	raw := h.ConfigJSON()
	if raw == "" {
		fresh, rerr := h.Refresh(ctx)
		if rerr != nil {
			return fmt.Errorf("msb: net-policy assert %q: refresh: %w", name, rerr)
		}
		raw = fresh.ConfigJSON()
	}
	return checkNetworkPolicy(name, raw)
}

func checkNetworkPolicy(name, raw string) error {
	if raw == "" {
		return fmt.Errorf("msb: net-policy assert %q: daemon returned an empty config record; cannot confirm egress containment", name)
	}
	var rec netPolicyRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return fmt.Errorf("msb: net-policy assert %q: parse daemon config record: %w", name, err)
	}
	if rec.Network == nil || rec.Network.Policy == nil {
		return fmt.Errorf("msb: net-policy assert %q: effective network policy absent from the daemon record (network=%v); refusing to hand back an unprotected sandbox", name, rec.Network != nil)
	}
	if got := msbsdk.PolicyAction(rec.Network.Policy.DefaultEgress); got != msbsdk.PolicyActionDeny {
		return fmt.Errorf("msb: net-policy assert %q: effective default_egress=%q, want %q", name, got, msbsdk.PolicyActionDeny)
	}
	return nil
}

func assertNetworkPolicyByName(ctx context.Context, name string) error {
	h, err := msbsdk.GetSandbox(ctx, name)
	if err != nil {
		return fmt.Errorf("msb: net-policy assert %q: get sandbox: %w", name, err)
	}
	return assertNetworkPolicy(ctx, h)
}

func teardownHandle(ctx context.Context, h *msbsdk.SandboxHandle) error {
	_ = h.Kill(ctx)
	if err := h.Remove(ctx); err != nil {
		return fmt.Errorf("msb: net-policy assert %q: teardown of unprotected sandbox failed: %w", h.Name(), err)
	}
	return nil
}

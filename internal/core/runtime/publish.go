package runtime

import (
	"context"
	"fmt"
	"slices"
)

type Publisher interface {
	Publish(ctx context.Context, spec SandboxSpec, ports []uint16) (SandboxRef, error)
}

func SamePortMap(ports []uint16) map[uint16]uint16 {
	m := make(map[uint16]uint16, len(ports))
	for _, p := range ports {
		m[p] = p
	}
	return m
}

func ValidatePorts(ports []uint16) error {
	seen := make(map[uint16]struct{}, len(ports))
	for _, p := range ports {
		if p == 0 {
			return fmt.Errorf("runtime: port 0 is not valid")
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("runtime: duplicate port %d", p)
		}
		seen[p] = struct{}{}
	}
	return nil
}

func MergePorts(existing, extra []uint16) []uint16 {
	seen := make(map[uint16]struct{}, len(existing)+len(extra))
	all := make([]uint16, 0, len(existing)+len(extra))
	all = append(all, existing...)
	all = append(all, extra...)
	var out []uint16
	for _, p := range all {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

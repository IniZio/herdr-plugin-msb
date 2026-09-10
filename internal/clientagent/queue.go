package clientagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	QueueFile  = "requests.json"
	AckFile    = "requests.acked"
	DefaultTTL = 10 * time.Minute
)

type Request struct {
	ID            uint64 `json:"id"`
	Host          string `json:"host"`
	RemotePort    uint16 `json:"remote_port"`
	LocalPort     uint16 `json:"local_port"`
	RemoteBind    string `json:"remote_bind"`
	Origin        string `json:"origin"`
	PaneID        string `json:"pane_id,omitempty"`
	OpenURL       string `json:"open_url,omitempty"`
	PluginVersion string `json:"plugin_version"`
	CreatedUnixMS uint64 `json:"created_unix_ms"`
}

type Queue struct {
	Version        uint32    `json:"version"`
	LastConsumedID uint64    `json:"last_consumed_id"`
	Pending        []Request `json:"pending"`
}

const remoteStateScan = `p=""; for d in "${XDG_STATE_HOME:-$HOME/.local/state}/` + StateDirNS + `" "${HERDR_PLUGIN_STATE_DIR:-}" "${XDG_STATE_HOME:-$HOME/.local/state}/herdr/plugins/` + PluginID + `"; do if [ -n "$d" ] && [ -f "$d/` + QueueFile + `" ]; then p="$d"; break; fi; done; `

const RemoteReadCommand = remoteStateScan + `if [ -n "$p" ]; then cat "$p/` + QueueFile + `"; fi`

const RemoteAckReadCommand = remoteStateScan + `if [ -n "$p" ]; then cat "$p/` + AckFile + `" 2>/dev/null; fi`

func RemoteMarkCommand(id uint64) string {
	n := strconv.FormatUint(id, 10)
	return remoteStateScan + `if [ -n "$p" ]; then o=$(cat "$p/` + AckFile + `" 2>/dev/null); case "$o" in ''|*[!0-9]*) o=0;; esac; if [ "$o" -ge ` + n + ` ] 2>/dev/null; then :; else t="$p/` + AckFile + `.$$"; printf '%s\n' ` + n + ` > "$t" && mv "$t" "$p/` + AckFile + `"; fi; fi`
}

func LoadQueue(dir string) (*Queue, error) {
	b, err := os.ReadFile(filepath.Join(dir, QueueFile))
	if errors.Is(err, os.ErrNotExist) {
		return &Queue{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	q := &Queue{}
	if err := json.Unmarshal(b, q); err != nil {
		return nil, fmt.Errorf("clientagent: parse %s: %w", QueueFile, err)
	}
	if q.Version == 0 {
		q.Version = 1
	}
	return q, nil
}

func WriteQueueAtomic(dir string, q *Queue) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, QueueFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, QueueFile))
}

func ReadAck(dir string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(dir, AckFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func ParseQueueJSON(s string) (*Queue, error) {
	q := &Queue{}
	t := strings.TrimSpace(s)
	if t == "" {
		return &Queue{Version: 1}, nil
	}
	if err := json.Unmarshal([]byte(t), q); err != nil {
		return nil, fmt.Errorf("clientagent: parse remote queue: %w", err)
	}
	return q, nil
}

func (q *Queue) NextID() uint64 {
	max := q.LastConsumedID
	for _, r := range q.Pending {
		if r.ID > max {
			max = r.ID
		}
	}
	return max + 1
}

func (q *Queue) Enqueue(r Request) Request {
	if r.ID == 0 {
		r.ID = q.NextID()
	}
	q.Pending = append(q.Pending, r)
	sort.Slice(q.Pending, func(i, j int) bool { return q.Pending[i].ID < q.Pending[j].ID })
	return r
}

func (q *Queue) Prune(acked uint64, now time.Time, ttl time.Duration) {
	cutoff := uint64(now.Add(-ttl).UnixMilli())
	kept := q.Pending[:0]
	for _, r := range q.Pending {
		if r.ID <= acked || r.CreatedUnixMS < cutoff {
			continue
		}
		kept = append(kept, r)
	}
	q.Pending = kept
	if acked > q.LastConsumedID {
		q.LastConsumedID = acked
	}
}

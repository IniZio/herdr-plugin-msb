//go:build linux

package cli

import (
	"time"

	"github.com/IniZio/herdr-plugin-msb/internal/clientagent"
	"github.com/IniZio/herdr-plugin-msb/internal/core/portfwd"
)

type Queue = clientagent.Queue
type Request = clientagent.Request
type Agent = clientagent.Agent

const (
	QueueFile         = clientagent.QueueFile
	AckFile           = clientagent.AckFile
	StateDirNS        = clientagent.StateDirNS
	MaxControlPathLen = clientagent.MaxControlPathLen
	PluginID          = clientagent.PluginID
	PluginVersion     = clientagent.PluginVersion
	DefaultPoll       time.Duration = clientagent.DefaultPoll
	DefaultTTL        time.Duration = clientagent.DefaultTTL
	RemoteReadCommand               = clientagent.RemoteReadCommand
	RemoteAckReadCommand            = clientagent.RemoteAckReadCommand
)

var (
	StateDir          = clientagent.StateDir
	ControlPathFor    = clientagent.ControlPathFor
	CheckControlPath  = clientagent.CheckControlPath
	MasterArgv        = clientagent.MasterArgv
	ExecArgv          = clientagent.ExecArgv
	ForwardArgv       = clientagent.ForwardArgv
	CancelArgv        = clientagent.CancelArgv
	ForwardSpec       = clientagent.ForwardSpec
	LoadQueue         = clientagent.LoadQueue
	WriteQueueAtomic  = clientagent.WriteQueueAtomic
	ReadAck           = clientagent.ReadAck
	ParseQueueJSON    = clientagent.ParseQueueJSON
	RemoteMarkCommand = clientagent.RemoteMarkCommand
	SpawnIfAbsent     = clientagent.SpawnIfAbsent
	AgentPidPath      = clientagent.AgentPidPath
	WaitForSocket     = clientagent.WaitForSocket
	TeardownSession   = clientagent.TeardownSession
	RunLocalAgentStartup = clientagent.RunLocalAgentStartup
)

type Runner = portfwd.Runner

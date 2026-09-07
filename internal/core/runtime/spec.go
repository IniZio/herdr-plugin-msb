package runtime

type SandboxStatus string

const (
	SandboxStatusCreated SandboxStatus = "created"
	SandboxStatusRunning SandboxStatus = "running"
	SandboxStatusPaused  SandboxStatus = "paused"
	SandboxStatusStopped SandboxStatus = "stopped"
)

type SandboxSpec struct {
	Project      string
	Name         string
	ImageRef     string
	VCPUs        uint32
	Motive       string
	RemoveOnExit bool
	Mounts       []Mount
	NetRules     []NetRule
}

type SandboxRef struct {
	ID      string
	Project string
	Name    string
	Status  SandboxStatus
}

package runtime

type NetAction string

const (
	NetAllow NetAction = "allow"
	NetDeny  NetAction = "deny"
)

type NetRule struct {
	Action NetAction
	Host   string
	Port   uint16
}

type ProxyEndpoint struct {
	Host string
	Port uint16
}

type Mount struct {
	HostPath  string
	GuestPath string
	ReadOnly  bool
}

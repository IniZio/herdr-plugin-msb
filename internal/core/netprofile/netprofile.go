package netprofile

import (
	"errors"

	"github.com/IniZio/herdr-plugin-msb/internal/core/runtime"
)

var ErrEmptyConfig = errors.New("netprofile: empty config produces no rules; use Closed() for an explicit deny-all")

type Entry struct {
	Host string
	Port uint16
}

type Config struct {
	Allow []Entry
}

var DefaultConfig = Config{
	Allow: []Entry{
		{Host: "api.anthropic.com", Port: 443},
	},
}

func Rules(c Config) ([]runtime.NetRule, error) {
	if len(c.Allow) == 0 {
		return nil, ErrEmptyConfig
	}
	rules := make([]runtime.NetRule, 0, len(c.Allow))
	for _, e := range c.Allow {
		rules = append(rules, runtime.NetRule{
			Action: runtime.NetAllow,
			Host:   e.Host,
			Port:   e.Port,
		})
	}
	return rules, nil
}

func Apply(spec *runtime.SandboxSpec) {
	spec.NetRules = Shipped()
}

func Shipped() []runtime.NetRule {
	rules, err := Rules(DefaultConfig)
	if err != nil {
		panic("netprofile: DefaultConfig is invalid: " + err.Error())
	}
	return rules
}

func Closed() []runtime.NetRule {
	return []runtime.NetRule{
		{Action: runtime.NetDeny, Host: "public", Port: 0},
	}
}

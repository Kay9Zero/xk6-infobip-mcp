// Package infobip_mcp contains the xk6-infobip-mcp extension.
package infobip_mcp

import (
	"github.com/sirupsen/logrus"
	"go.k6.io/k6/js/modules"
)

type rootModule struct{}

func (*rootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	env := vu.InitEnv()
	return &module{
		vu:      vu,
		logger:  env.Logger,
		metrics: newMCPMetrics(env),
	}
}

type module struct {
	vu      modules.VU
	logger  logrus.FieldLogger
	metrics *MCPMetrics
}

func (m *module) Exports() modules.Exports {
	return modules.Exports{
		Named: map[string]any{
			"NewClient": m.newClient,
		},
	}
}

var _ modules.Module = (*rootModule)(nil)

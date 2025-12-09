package infobip_mcp

import (
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/metrics"
)

type MCPMetrics struct {
	// MCP metrics
	MCPCallDuration *metrics.Metric
	MCPCalls        *metrics.Metric
	MCPSuccess      *metrics.Metric
	MCPErrors       *metrics.Metric

	// HTTP metrics
	HTTPRequestDuration *metrics.Metric
	HTTPRequestCount    *metrics.Metric
	HTTPRequestErrors   *metrics.Metric
	HTTPRequestSize     *metrics.Metric

	TagsAndMeta *metrics.TagsAndMeta
}

// newMCPMetrics creates and initializes a new MCPMetrics instance with all required
// MCP and HTTP metrics for tracking performance and success rates.
func newMCPMetrics(env *common.InitEnvironment) *MCPMetrics {
	return &MCPMetrics{
		MCPCallDuration: env.Registry.MustNewMetric("mcp_call_duration", metrics.Trend, metrics.Time),
		MCPCalls:        env.Registry.MustNewMetric("mcp_calls", metrics.Counter),
		MCPSuccess:      env.Registry.MustNewMetric("mcp_success", metrics.Rate),
		MCPErrors:       env.Registry.MustNewMetric("mcp_errors", metrics.Rate),

		HTTPRequestDuration: env.BuiltinMetrics.HTTPReqDuration,
		HTTPRequestCount:    env.BuiltinMetrics.HTTPReqs,
		HTTPRequestErrors:   env.BuiltinMetrics.HTTPReqFailed,

		TagsAndMeta: &metrics.TagsAndMeta{
			Tags: env.Registry.RootTagSet(),
		},
	}
}

package infobip_mcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/sobek"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	"go.k6.io/k6/v2/js/common"
	"go.k6.io/k6/v2/js/modules"
	"go.k6.io/k6/v2/metrics"
)

type MCPClient struct {
	session         *mcp.ClientSession
	ctx             context.Context
	cancel          context.CancelFunc
	toolCallTimeout time.Duration
	vu              modules.VU
	logger          logrus.FieldLogger
	metrics         *MCPMetrics
}

type ClientConfig struct {
	Endpoint string
	Timeout  int64
	IsSSE    bool
	Headers  map[string]string
}

// newClient creates a new MCP (Model Context Protocol) client with the provided configuration.
func (m *module) newClient(c sobek.ConstructorCall, rt *sobek.Runtime) *sobek.Object {
	m.logger.Debugf("Setting up new MCP client")

	var cfg ClientConfig
	if err := rt.ExportTo(c.Argument(0), &cfg); err != nil {
		common.Throw(rt, fmt.Errorf("invalid config: %w", err))
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 2
	}

	m.logger.Debugf("newClient started: Endpoint=%s, isSSE=%v, timeout=%v", cfg.Endpoint, cfg.IsSSE, cfg.Timeout)

	client := mcp.NewClient(&mcp.Implementation{Name: "xk6-infobip-mcp", Version: "v1.0.0"}, nil)
	// The session keeps using this context after Connect returns - the SSE
	// receive loop reads from a request body built with it - so it has to
	// live as long as the client does. The connect deadline is enforced by a
	// timer instead, and stopped once the handshake succeeds.
	ctx, cancel := context.WithCancel(context.Background())
	connectTimer := time.AfterFunc(time.Duration(cfg.Timeout)*time.Second, cancel)

	var tlsConfig *tls.Config
	if m.vu.State().TLSConfig != nil {
		tlsConfig = m.vu.State().TLSConfig.Clone()
		tlsConfig.NextProtos = []string{"http/1.1"}
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext:       m.vu.State().Dialer.DialContext,
			Proxy:             http.ProxyFromEnvironment,
			TLSClientConfig:   tlsConfig,
			DisableKeepAlives: m.vu.State().Options.NoConnectionReuse.ValueOrZero() || m.vu.State().Options.NoVUConnectionReuse.ValueOrZero(),
		},
	}

	httpClient.Transport = RoundTripper{
		headers:   cfg.Headers,
		transport: httpClient.Transport,
		state:     m.vu.State(),
		metrics:   m.metrics,
	}

	if len(cfg.Headers) > 0 {
		m.logger.Debugf("Adding %d custom headers to HTTP client", len(cfg.Headers))
	}

	var session *mcp.ClientSession
	var sessionErr error
	if cfg.IsSSE {
		sseTransport := &mcp.SSEClientTransport{
			Endpoint:   cfg.Endpoint,
			HTTPClient: httpClient,
		}
		session, sessionErr = client.Connect(ctx, sseTransport, &mcp.ClientSessionOptions{})
	} else {
		streamableTransport := &mcp.StreamableClientTransport{
			Endpoint:   cfg.Endpoint,
			HTTPClient: httpClient,
			MaxRetries: -1,
		}
		session, sessionErr = client.Connect(ctx, streamableTransport, &mcp.ClientSessionOptions{})
	}

	if !connectTimer.Stop() {
		// The deadline fired. Close a session that won the race, and report a
		// timeout unless Connect had already failed for an unrelated reason.
		if session != nil {
			_ = session.Close()
		}
		if sessionErr == nil || errors.Is(sessionErr, context.Canceled) {
			sessionErr = context.DeadlineExceeded
		}
	}

	if sessionErr != nil {
		cancel()
		common.Throw(rt, fmt.Errorf("failed to connect: %w", sessionErr))
	}

	mcpClient := &MCPClient{
		session:         session,
		ctx:             ctx,
		cancel:          cancel,
		toolCallTimeout: time.Duration(cfg.Timeout) * time.Second,
		vu:              m.vu,
		logger:          m.logger,
		metrics:         m.metrics,
	}

	return rt.ToValue(mcpClient).ToObject(rt)
}

// CloseConnection terminates the MCP client session and cleans up resources.
func (client *MCPClient) CloseConnection() error {
	if client.cancel != nil {
		defer client.cancel()
	}

	return client.session.Close()
}

// pushMetrics records performance metrics for MCP tool calls.
func (client *MCPClient) pushMetrics(mcpAction string, duration time.Duration, isError bool) {
	state := client.vu.State()
	tags := state.Tags.GetCurrentValues().Tags.With(
		"method", mcpAction,
	)
	metrics.PushIfNotDone(context.Background(), state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: client.metrics.MCPCallDuration,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: float64(duration) / float64(time.Millisecond),
	})

	metrics.PushIfNotDone(context.Background(), state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: client.metrics.MCPCalls,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: 1,
	})

	errValue := 0
	successValue := 1

	if isError {
		errValue = 1
		successValue = 0
	}

	metrics.PushIfNotDone(context.Background(), state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: client.metrics.MCPErrors,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: float64(errValue),
	})

	metrics.PushIfNotDone(context.Background(), client.vu.State().Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: client.metrics.MCPSuccess,
			Tags:   tags,
		},
		Time:  time.Now(),
		Value: float64(successValue),
	})
}

// CallTool invokes a named tool through the MCP protocol with the provided arguments.
func (client *MCPClient) CallTool(toolName string, args map[string]any, rt *sobek.Runtime) string {
	if client.session == nil {
		common.Throw(rt, errors.New("MCP client not initialized. Call NewClient first"))
	}

	params := &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	}

	client.logger.Debugf("Calling tool: %s", toolName)

	ctx, cancel := context.WithTimeout(client.ctx, client.toolCallTimeout)
	defer cancel()

	start := time.Now().UTC()
	res, err := client.session.CallTool(ctx, params)
	callDuration := time.Since(start)

	if err != nil {
		client.logger.Debugf("Tool call failed after %v: %v", callDuration, err)
		client.pushMetrics(toolName, time.Since(start), true)
		return ""
	}

	var response strings.Builder
	for _, content := range res.Content {
		textContent, ok := content.(*mcp.TextContent)
		if ok {
			response.WriteString(textContent.Text)
		}
	}
	txtResponse := response.String()

	client.logger.Debugf("=== MCP TOOL CALL ===")
	client.logger.Debugf("Tool Name: %s", toolName)
	client.logger.Debugf("Arguments: %+v", args)
	client.logger.Debugf("Response IsError: %v", res.IsError)
	client.logger.Debugf("Content text: %s", txtResponse)
	client.logger.Debugf("=====================")

	client.pushMetrics(toolName, time.Since(start), res.IsError)
	return txtResponse
}

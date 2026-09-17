package infobip_mcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/sobek"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modulestest"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

type Input struct {
	Name string `json:"name" jsonschema:"the name of the person to greet"`
}

type Output struct {
	Greeting string `json:"greeting" jsonschema:"the greeting to tell to the user"`
}

func SayHi(ctx context.Context, req *mcp.CallToolRequest, input Input) (
	*mcp.CallToolResult,
	Output,
	error,
) {
	return nil, Output{Greeting: "Hi " + input.Name}, nil
}

func newTestMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "greeter", Version: "v1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "say hi"}, SayHi)

	inputSchema := map[string]any{"type": "object"}
	server.AddTool(&mcp.Tool{Name: "mixed", InputSchema: inputSchema}, func(
		context.Context,
		*mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: "first"},
			&mcp.ImageContent{Data: []byte("image"), MIMEType: "image/png"},
			&mcp.TextContent{Text: "second"},
		}}, nil
	})
	server.AddTool(&mcp.Tool{Name: "tool_error", InputSchema: inputSchema}, func(
		context.Context,
		*mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "tool failed"}},
			IsError: true,
		}, nil
	})
	server.AddTool(&mcp.Tool{Name: "protocol_error", InputSchema: inputSchema}, func(
		context.Context,
		*mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return nil, errors.New("protocol failure")
	})

	return server
}

type moduleTestRuntime struct {
	runtime *modulestest.Runtime
	samples chan metrics.SampleContainer
}

func newModuleTestRuntime(t *testing.T, tlsConfig *tls.Config) *moduleTestRuntime {
	t.Helper()

	runtime := modulestest.NewRuntime(t)
	t.Cleanup(runtime.CancelContext)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	registry := metrics.NewRegistry()
	builtinMetrics := metrics.RegisterBuiltinMetrics(registry)
	runtime.VU.InitEnvField = &common.InitEnvironment{
		TestPreInitState: &lib.TestPreInitState{
			Registry:       registry,
			BuiltinMetrics: builtinMetrics,
			Logger:         logger,
		},
	}

	instance := new(rootModule).NewModuleInstance(runtime.VU)
	require.NoError(t, runtime.VU.Runtime().Set("mcp", instance.Exports().Named))

	runtime.VU.InitEnvField = nil
	samples := make(chan metrics.SampleContainer, 1000)
	runtime.MoveToVUContext(&lib.State{
		Options: lib.Options{
			SystemTags: &metrics.DefaultSystemTagSet,
		},
		Samples:        samples,
		Tags:           lib.NewVUStateTags(registry.RootTagSet()),
		BuiltinMetrics: builtinMetrics,
		Dialer:         &net.Dialer{},
		TLSConfig:      tlsConfig,
		Logger:         logger,
	})

	return &moduleTestRuntime{runtime: runtime, samples: samples}
}

func (r *moduleTestRuntime) run(t *testing.T, code string) sobek.Value {
	t.Helper()

	value, err := r.runtime.RunOnEventLoop(code)
	require.NoError(t, err)

	return value
}

func collectSamples(input <-chan metrics.SampleContainer) []metrics.Sample {
	var samples []metrics.Sample
	for _, container := range metrics.GetBufferedSamples(input) {
		samples = append(samples, container.GetSamples()...)
	}

	return samples
}

func findSample(t *testing.T, samples []metrics.Sample, metricName, method string) metrics.Sample {
	t.Helper()

	for _, sample := range samples {
		if sample.Metric.Name != metricName {
			continue
		}
		if value, ok := sample.Tags.Get("method"); ok && value == method {
			return sample
		}
	}

	require.FailNow(t, "metric sample not found", "metric=%s method=%s", metricName, method)

	return metrics.Sample{}
}

func requireCallMetrics(t *testing.T, samples []metrics.Sample, method string, wantError bool) {
	t.Helper()

	require.Equal(t, 1.0, findSample(t, samples, "mcp_calls", method).Value)
	require.GreaterOrEqual(t, findSample(t, samples, "mcp_call_duration", method).Value, 0.0)

	wantErrorValue := 0.0
	wantSuccessValue := 1.0
	if wantError {
		wantErrorValue = 1.0
		wantSuccessValue = 0.0
	}
	require.Equal(t, wantErrorValue, findSample(t, samples, "mcp_errors", method).Value)
	require.Equal(t, wantSuccessValue, findSample(t, samples, "mcp_success", method).Value)
}

func TestStreamableClient(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	var headerSeen atomic.Bool
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Test-Header") == "present" {
			headerSeen.Store(true)
		}
		handler.ServeHTTP(w, req)
	}))
	t.Cleanup(httpServer.Close)

	runtime := newModuleTestRuntime(t, nil)
	code := fmt.Sprintf(`(() => {
		const client = mcp.NewClient({
			endpoint: %q,
			timeout: 5,
			headers: {"X-Test-Header": "present"},
		});
		const greeting = JSON.parse(client.callTool("greet", {"name": "k6"})).greeting;
		const mixed = client.callTool("mixed", {});
		const toolError = client.callTool("tool_error", {});
		client.closeConnection();
		return greeting === "Hi k6" && mixed === "firstsecond" && toolError === "tool failed";
	})()`, httpServer.URL)
	require.True(t, runtime.run(t, code).ToBoolean())
	require.True(t, headerSeen.Load())

	samples := collectSamples(runtime.samples)
	requireCallMetrics(t, samples, "greet", false)
	requireCallMetrics(t, samples, "mixed", false)
	requireCallMetrics(t, samples, "tool_error", true)
}

func TestStreamableClientProtocolError(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil))
	t.Cleanup(httpServer.Close)

	runtime := newModuleTestRuntime(t, nil)
	code := fmt.Sprintf(`(() => {
		const client = mcp.NewClient({endpoint: %q, timeout: 5});
		const result = client.callTool("protocol_error", {});
		client.closeConnection();
		return result;
	})()`, httpServer.URL)
	require.Empty(t, runtime.run(t, code).String())
	requireCallMetrics(t, collectSamples(runtime.samples), "protocol_error", true)
}

func TestSSEClient(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	runtime := newModuleTestRuntime(t, nil)
	code := fmt.Sprintf(`(() => {
		const client = mcp.NewClient({endpoint: %q, timeout: 30, isSSE: true});
		const greeting = JSON.parse(client.callTool("greet", {"name": "SSE"})).greeting;
		client.closeConnection();
		return greeting;
	})()`, httpServer.URL)
	require.Equal(t, "Hi SSE", runtime.run(t, code).String())
}

// TestDefaultTimeout asserts the fallback applied when the script omits
// timeout. It inspects the constructed client rather than measuring elapsed
// time, so a slow CI runner cannot turn the default into a flaky failure.
func TestDefaultTimeout(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil))
	t.Cleanup(httpServer.Close)

	runtime := newModuleTestRuntime(t, nil)
	value := runtime.run(t, fmt.Sprintf(`mcp.NewClient({endpoint: %q})`, httpServer.URL))

	client, ok := value.Export().(*MCPClient)
	require.True(t, ok, "NewClient should return an *MCPClient")
	require.Equal(t, 2*time.Second, client.toolCallTimeout)
	require.NoError(t, client.CloseConnection())
}

func TestTLSClient(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	httpServer := httptest.NewTLSServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil))
	t.Cleanup(httpServer.Close)

	transport := httpServer.Client().Transport.(*http.Transport)
	runtime := newModuleTestRuntime(t, transport.TLSClientConfig.Clone())
	code := fmt.Sprintf(`(() => {
		const client = mcp.NewClient({endpoint: %q, timeout: 5});
		const greeting = JSON.parse(client.callTool("greet", {"name": "TLS"})).greeting;
		client.closeConnection();
		return greeting;
	})()`, httpServer.URL)
	require.Equal(t, "Hi TLS", runtime.run(t, code).String())
}

func TestNewClientErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()

		runtime := newModuleTestRuntime(t, nil)
		_, err := runtime.runtime.RunOnEventLoop(`mcp.NewClient("invalid")`)
		require.ErrorContains(t, err, "invalid config")
	})

	t.Run("connection failure", func(t *testing.T) {
		t.Parallel()

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		endpoint := "http://" + listener.Addr().String()
		require.NoError(t, listener.Close())

		runtime := newModuleTestRuntime(t, nil)
		_, err = runtime.runtime.RunOnEventLoop(fmt.Sprintf(
			`mcp.NewClient({endpoint: %q, timeout: 1})`,
			endpoint,
		))
		require.ErrorContains(t, err, "failed to connect")
	})

	t.Run("connection timeout", func(t *testing.T) {
		t.Parallel()

		release := make(chan struct{})
		httpServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			<-release
		}))
		defer func() {
			close(release)
			httpServer.Close()
		}()

		runtime := newModuleTestRuntime(t, nil)
		started := time.Now()
		_, err := runtime.runtime.RunOnEventLoop(fmt.Sprintf(
			`mcp.NewClient({endpoint: %q, timeout: 1, isSSE: true})`,
			httpServer.URL,
		))
		require.ErrorContains(t, err, "context deadline exceeded")
		// The point is that the deadline is enforced at all rather than the
		// connect hanging indefinitely, so keep the bound well clear of the
		// one second timeout: a loaded CI runner must not turn this into a
		// flake.
		require.Less(t, time.Since(started), 15*time.Second)
	})
}

func TestCallToolRequiresSession(t *testing.T) {
	t.Parallel()

	runtime := sobek.New()
	client := new(MCPClient)
	require.Panics(t, func() {
		client.CallTool("greet", nil, runtime)
	})
}

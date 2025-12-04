package infobip_mcp

import (
	"context"
	_ "embed"
	"log"
	"net"
	"net/http"
	"testing"

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

func setupTestMCPServer() chan bool {
	server := mcp.NewServer(&mcp.Implementation{Name: "greeter", Version: "v1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "say hi"}, SayHi)
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return server
	}, nil)

	ready := make(chan bool)

	go func() {
		listener, err := net.Listen("tcp", "127.0.0.1:4000")
		if err != nil {
			log.Fatalf("Failed to listen: %v", err)
		}

		ready <- true

		if err := http.Serve(listener, handler); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	return ready
}

func Test_module(t *testing.T) { //nolint:tparallel
	ready := setupTestMCPServer()
	<-ready
	t.Parallel()

	runtime := modulestest.NewRuntime(t)

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	registry := metrics.NewRegistry()
	builtinMetrics := metrics.RegisterBuiltinMetrics(registry)

	runtime.VU.InitEnvField = &common.InitEnvironment{
		TestPreInitState: &lib.TestPreInitState{
			Registry:       registry,
			BuiltinMetrics: builtinMetrics,
			Logger:         logger,
		},
	}

	m := new(rootModule).NewModuleInstance(runtime.VU)
	require.NoError(t, runtime.VU.Runtime().Set("mcp", m.Exports().Named))

	runtime.VU.InitEnvField = nil
	samples := make(chan metrics.SampleContainer, 1000)
	state := &lib.State{
		Options: lib.Options{
			SystemTags: &metrics.DefaultSystemTagSet,
		},
		Samples:        samples,
		Tags:           lib.NewVUStateTags(registry.RootTagSet().WithTagsFromMap(map[string]string{"group": lib.RootGroupPath})),
		BuiltinMetrics: builtinMetrics,
		Dialer:         &net.Dialer{},
	}
	runtime.MoveToVUContext(state)

	tests := []struct {
		name  string
		check string
	}{
		{
			name:  "NewClient().callTool()",
			check: `JSON.parse(mcp.NewClient({endpoint: "http://127.0.0.1:4000"}).callTool("greet", {"name": "k6"})).greeting === "Hi k6"`,
		},
	}

	for _, tt := range tests { //nolint:paralleltest
		t.Run(tt.name, func(t *testing.T) {
			got, err := runtime.RunOnEventLoop(tt.check)
			require.NoError(t, err)
			require.True(t, got.ToBoolean())
		})
	}
}

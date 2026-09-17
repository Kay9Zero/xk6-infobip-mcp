package infobip_mcp

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRoundTripperMetrics(t *testing.T) {
	t.Parallel()

	transportError := errors.New("transport failure")
	tests := []struct {
		name        string
		method      string
		status      int
		err         error
		wantFailure float64
	}{
		{name: "success", method: http.MethodPost, status: http.StatusOK, wantFailure: 0},
		{name: "redirect", method: http.MethodGet, status: http.StatusTemporaryRedirect, wantFailure: 0},
		{name: "SSE probe not allowed", method: http.MethodGet, status: http.StatusMethodNotAllowed, wantFailure: 0},
		{name: "session already gone", method: http.MethodDelete, status: http.StatusNotFound, wantFailure: 0},
		{name: "client error", method: http.MethodPost, status: http.StatusBadRequest, wantFailure: 1},
		{name: "ordinary not found", method: http.MethodGet, status: http.StatusNotFound, wantFailure: 1},
		{name: "server error", method: http.MethodPost, status: http.StatusInternalServerError, wantFailure: 1},
		{name: "transport error", method: http.MethodPost, err: transportError, wantFailure: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			registry := metrics.NewRegistry()
			builtinMetrics := metrics.RegisterBuiltinMetrics(registry)
			extensionMetrics := newMCPMetrics(&common.InitEnvironment{
				TestPreInitState: &lib.TestPreInitState{
					Registry:       registry,
					BuiltinMetrics: builtinMetrics,
				},
			})
			sampleChannel := make(chan metrics.SampleContainer, 3)
			state := &lib.State{
				Samples: sampleChannel,
				Tags:    lib.NewVUStateTags(registry.RootTagSet()),
			}
			headerSeen := false
			transport := RoundTripper{
				headers: map[string]string{"X-Test-Header": "present"},
				metrics: extensionMetrics,
				state:   state,
				transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					headerSeen = true
					if test.err != nil {
						return nil, test.err
					}
					return &http.Response{
						StatusCode: test.status,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}),
			}

			req, err := http.NewRequest(test.method, "https://example.test/mcp?case=metrics", nil)
			require.NoError(t, err)
			response, gotErr := transport.RoundTrip(req)
			if test.err == nil {
				require.NoError(t, gotErr)
				require.Equal(t, test.status, response.StatusCode)
			} else {
				require.ErrorIs(t, gotErr, test.err)
				require.Nil(t, response)
			}
			require.True(t, headerSeen)
			require.Equal(t, "present", req.Header.Get("X-Test-Header"))

			samples := collectSamples(sampleChannel)
			require.Len(t, samples, 3)
			byName := make(map[string]metrics.Sample, len(samples))
			for _, sample := range samples {
				byName[sample.Metric.Name] = sample
				require.Equal(t, test.method, sample.Tags.Map()["method"])
				require.Equal(t, "https://example.test/mcp?case=metrics", sample.Tags.Map()["url"])
			}
			require.Equal(t, test.wantFailure, byName["http_req_failed"].Value)
			require.Equal(t, 1.0, byName["http_reqs"].Value)
			require.GreaterOrEqual(t, byName["http_req_duration"].Value, 0.0)
			require.Equal(t, test.status, statusTagAsInt(t, byName["http_reqs"]))
		})
	}
}

func statusTagAsInt(t *testing.T, sample metrics.Sample) int {
	t.Helper()

	code, err := strconv.Atoi(sample.Tags.Map()["status"])
	require.NoError(t, err)

	return code
}

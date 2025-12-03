package infobip_mcp

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"go.k6.io/k6/lib"
	"go.k6.io/k6/metrics"
)

type RoundTripper struct {
	headers   map[string]string
	transport http.RoundTripper
	metrics   *MCPMetrics
	state     *lib.State
}

// RoundTrip implements the http.RoundTripper interface. It intercepts HTTP requests
// to add custom headers and collect detailed metrics
func (r RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Add custom headers
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := r.transport.RoundTrip(req)
	duration := time.Since(start)

	statusCode := 0
	var responseSize int64
	if resp != nil {
		statusCode = resp.StatusCode
		responseSize = max(resp.ContentLength, 0)
	}

	tags := r.state.Tags.GetCurrentValues().Tags.WithTagsFromMap(map[string]string{
		"method": req.Method,
		"url":    req.URL.String(),
		"status": strconv.Itoa(statusCode),
	})
	now := time.Now()

	metrics.PushIfNotDone(context.Background(), r.state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: r.metrics.HTTPRequestDuration,
			Tags:   tags,
		},
		Time:  now,
		Value: float64(duration) / float64(time.Millisecond),
	})

	metrics.PushIfNotDone(context.Background(), r.state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: r.metrics.HTTPRequestCount,
			Tags:   tags,
		},
		Time:  now,
		Value: 1,
	})

	failureValue := 1.0
	if err == nil {
		if statusCode == 404 && req.Method == "DELETE" {
			failureValue = 0.0
		}

		if statusCode == 405 && req.Method == "GET" {
			failureValue = 0.0
		}

		if statusCode < 400 {
			failureValue = 0.0
		}
	}

	metrics.PushIfNotDone(context.Background(), r.state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: r.metrics.HTTPRequestErrors,
			Tags:   tags,
		},
		Time:  now,
		Value: failureValue,
	})

	metrics.PushIfNotDone(context.Background(), r.state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{
			Metric: r.metrics.HTTPResponseSize,
			Tags:   tags,
		},
		Time:  now,
		Value: float64(responseSize),
	})

	return resp, err
}

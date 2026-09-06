package ops

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetricsRenderContainsOnlyAggregatedLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveHTTP("GET", 200, 150*time.Millisecond)
	var output bytes.Buffer
	if err := metrics.Render(&output, true); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		"mindfs_cloud_up 1",
		"mindfs_cloud_ready 1",
		`mindfs_cloud_http_requests_total{method="GET",status="200"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"path=", "query=", "authorization", "token"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("metrics exposed %q: %s", forbidden, text)
		}
	}
}

func TestMetricsBoundsUntrustedLabels(t *testing.T) {
	metrics := NewMetrics()
	for i := range 20000 {
		metrics.ObserveHTTP(fmt.Sprintf("CUSTOM%d", i), 404, time.Millisecond)
		metrics.ObserveHTTP("GET", 1000+i, time.Millisecond)
	}
	if len(metrics.values) != 2 {
		t.Fatalf("unbounded metric groups: %d", len(metrics.values))
	}
	if got := metrics.values[metricKey{method: "OTHER", status: 404}]; got.count != 20000 || got.duration != 20*time.Second {
		t.Fatalf("custom method totals lost: %+v", got)
	}
	var output bytes.Buffer
	if err := metrics.Render(&output, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "CUSTOM") {
		t.Fatal("raw custom method escaped into metrics")
	}
}

func TestMetricsConcurrentObserveAndRender(t *testing.T) {
	metrics := NewMetrics()
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 1000 {
				metrics.ObserveHTTP("GET", 200, time.Millisecond)
			}
		})
	}
	for range 100 {
		if err := metrics.Render(io.Discard, true); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	if got := metrics.values[metricKey{method: "GET", status: 200}]; got.count != 4000 || got.duration != 4*time.Second {
		t.Fatalf("concurrent totals lost: %+v", got)
	}
}

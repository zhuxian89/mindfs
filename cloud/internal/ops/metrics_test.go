package ops

import (
	"bytes"
	"strings"
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

package ops

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

type metricKey struct {
	method string
	status int
}

type metricValue struct {
	count    uint64
	duration time.Duration
}

type Metrics struct {
	mu     sync.Mutex
	values map[metricKey]metricValue
}

func NewMetrics() *Metrics {
	return &Metrics{values: make(map[metricKey]metricValue)}
}

func (m *Metrics) ObserveHTTP(method string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	key := metricKey{method: method, status: status}
	m.mu.Lock()
	value := m.values[key]
	value.count++
	value.duration += duration
	m.values[key] = value
	m.mu.Unlock()
}

func (m *Metrics) Render(w io.Writer, ready bool) error {
	m.mu.Lock()
	keys := make([]metricKey, 0, len(m.values))
	values := make(map[metricKey]metricValue, len(m.values))
	for key, value := range m.values {
		keys = append(keys, key)
		values[key] = value
	}
	m.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method == keys[j].method {
			return keys[i].status < keys[j].status
		}
		return keys[i].method < keys[j].method
	})
	readyValue := 0
	if ready {
		readyValue = 1
	}
	if _, err := fmt.Fprintln(w, "# TYPE mindfs_cloud_up gauge\nmindfs_cloud_up 1"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "# TYPE mindfs_cloud_ready gauge\nmindfs_cloud_ready %d\n", readyValue); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE mindfs_cloud_http_requests_total counter"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# TYPE mindfs_cloud_http_request_duration_seconds summary"); err != nil {
		return err
	}
	for _, key := range keys {
		value := values[key]
		labels := fmt.Sprintf("method=%q,status=%q", key.method, fmt.Sprint(key.status))
		if _, err := fmt.Fprintf(w, "mindfs_cloud_http_requests_total{%s} %d\n", labels, value.count); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "mindfs_cloud_http_request_duration_seconds_sum{%s} %.9f\n", labels, value.duration.Seconds()); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "mindfs_cloud_http_request_duration_seconds_count{%s} %d\n", labels, value.count); err != nil {
			return err
		}
	}
	return nil
}

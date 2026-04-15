package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/Talbot3/go-tunnel/internal/metrics"
)

func TestNewCollector(t *testing.T) {
	// Use a new registry to avoid conflicts
	registry := prometheus.NewRegistry()

	c := metrics.NewCollector(metrics.Config{
		Namespace: "test_tunnel",
		Registry:  registry,
	})

	if c == nil {
		t.Fatal("Expected non-nil collector")
	}
}

func TestCollector_ConnectionMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace: "test_conn",
		Registry:  registry,
	})

	// Test connection counters
	c.IncConnections()
	c.IncConnectionsBy(5)

	// Test active connections gauge
	c.IncActive()
	c.IncActive()
	c.DecActive()
	c.SetActive(10)
}

func TestCollector_TrafficMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace: "test_traffic",
		Registry:  registry,
	})

	// Test traffic counters
	c.AddBytesSent(1024)
	c.AddBytesSent(2048)
	c.AddBytesReceived(512)
}

func TestCollector_ErrorMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace: "test_errors",
		Registry:  registry,
	})

	// Test error counters
	c.IncErrors()
	c.IncErrorsBy(3)
}

func TestCollector_LatencyMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace:                 "test_latency",
		Registry:                  registry,
		EnableConnectionDuration:  true,
		EnableForwardLatency:      true,
	})

	// Test latency histograms
	c.ObserveConnectionDuration(0.5)
	c.ObserveConnectionDuration(1.0)
	c.ObserveForwardLatency(0.01)
	c.ObserveForwardLatency(0.02)
}

func TestCollector_PoolMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace:         "test_pool",
		Registry:          registry,
		EnablePoolMetrics: true,
	})

	// Test pool metrics
	c.SetPoolActive(5)
	c.IncPoolCreated()
	c.IncPoolReused()
	c.IncPoolClosed()
	c.ObservePoolWait(0.001)
}

func TestCollector_BackpressureMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace:                 "test_bp",
		Registry:                  registry,
		EnableBackpressureMetrics: true,
	})

	// Test backpressure metrics
	c.IncBackpressurePauses()
	c.AddBackpressureYieldTime(0.05)
}

func TestCollector_DisabledMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace:                 "test_disabled",
		Registry:                  registry,
		EnableConnectionDuration:  false,
		EnableForwardLatency:      false,
		EnablePoolMetrics:         false,
		EnableBackpressureMetrics: false,
	})

	// These should not panic even though metrics are disabled
	c.ObserveConnectionDuration(0.5)
	c.ObserveForwardLatency(0.01)
	c.SetPoolActive(5)
	c.IncBackpressurePauses()
}

func TestDefaultCollector(t *testing.T) {
	registry := prometheus.NewRegistry()
	c := metrics.NewCollector(metrics.Config{
		Namespace: "test_default",
		Registry:  registry,
		EnableConnectionDuration:  true,
		EnableForwardLatency:      true,
		EnablePoolMetrics:         true,
		EnableBackpressureMetrics: true,
	})

	if c == nil {
		t.Fatal("Expected non-nil collector")
	}
}

// Package metrics provides Prometheus metrics for the tunnel library.
//
// This package implements optional Prometheus metrics export for monitoring
// tunnel performance and health.
//
// # Example
//
//	metrics := metrics.NewCollector("go_tunnel")
//	metrics.Register()
//
//	// In your tunnel handler
//	metrics.IncConnections()
//	metrics.AddBytesSent(1024)
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Collector holds Prometheus metrics for a tunnel.
type Collector struct {
	namespace string

	// Counters
	connectionsTotal prometheus.Counter
	errorsTotal      prometheus.Counter

	// Gauges
	connectionsActive prometheus.Gauge

	// Counters for traffic
	bytesSent     prometheus.Counter
	bytesReceived prometheus.Counter

	// Histograms for latency
	connectionDuration prometheus.Histogram
	forwardLatency     prometheus.Histogram

	// Connection pool metrics
	poolConnectionsActive  prometheus.Gauge
	poolConnectionsTotal   prometheus.Counter
	poolConnectionsCreated prometheus.Counter
	poolConnectionsReused  prometheus.Counter
	poolConnectionsClosed  prometheus.Counter
	poolWaitDuration       prometheus.Histogram

	// Backpressure metrics
	backpressurePauses    prometheus.Counter
	backpressureYieldTime prometheus.Counter

	// Registry for custom registration
	registry prometheus.Registerer
}

// Config holds configuration for the metrics collector.
type Config struct {
	// Namespace is the prefix for all metrics.
	Namespace string

	// Subsystem is an optional subsystem prefix.
	Subsystem string

	// Registry is the Prometheus registerer to use.
	// If nil, the default registerer is used.
	Registry prometheus.Registerer

	// EnableConnectionDuration enables connection duration histogram.
	EnableConnectionDuration bool

	// EnableForwardLatency enables forward latency histogram.
	EnableForwardLatency bool

	// EnablePoolMetrics enables connection pool metrics.
	EnablePoolMetrics bool

	// EnableBackpressureMetrics enables backpressure metrics.
	EnableBackpressureMetrics bool
}

// NewCollector creates a new metrics collector.
func NewCollector(cfg Config) *Collector {
	if cfg.Namespace == "" {
		cfg.Namespace = "go_tunnel"
	}

	registry := cfg.Registry
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	c := &Collector{
		namespace: cfg.Namespace,
		registry:  registry,
	}

	// Initialize counters
	c.connectionsTotal = promauto.With(registry).NewCounter(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "connections_total",
		Help:      "Total number of connections handled",
	})

	c.errorsTotal = promauto.With(registry).NewCounter(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "errors_total",
		Help:      "Total number of errors encountered",
	})

	// Initialize gauges
	c.connectionsActive = promauto.With(registry).NewGauge(prometheus.GaugeOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "connections_active",
		Help:      "Number of currently active connections",
	})

	// Initialize traffic counters
	c.bytesSent = promauto.With(registry).NewCounter(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "bytes_sent_total",
		Help:      "Total bytes sent from source to target",
	})

	c.bytesReceived = promauto.With(registry).NewCounter(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      "bytes_received_total",
		Help:      "Total bytes received from target to source",
	})

	// Initialize histograms
	if cfg.EnableConnectionDuration {
		c.connectionDuration = promauto.With(registry).NewHistogram(prometheus.HistogramOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem,
			Name:      "connection_duration_seconds",
			Help:      "Duration of connections in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.01, 2, 15),
		})
	}

	if cfg.EnableForwardLatency {
		c.forwardLatency = promauto.With(registry).NewHistogram(prometheus.HistogramOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem,
			Name:      "forward_latency_seconds",
			Help:      "Latency of forward operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
		})
	}

	// Initialize pool metrics
	if cfg.EnablePoolMetrics {
		c.poolConnectionsActive = promauto.With(registry).NewGauge(prometheus.GaugeOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_pool",
			Name:      "connections_active",
			Help:      "Number of active connections in the pool",
		})

		c.poolConnectionsTotal = promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_pool",
			Name:      "connections_total",
			Help:      "Total connections in the pool",
		})

		c.poolConnectionsCreated = promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_pool",
			Name:      "connections_created_total",
			Help:      "Total connections created in the pool",
		})

		c.poolConnectionsReused = promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_pool",
			Name:      "connections_reused_total",
			Help:      "Total connections reused from the pool",
		})

		c.poolConnectionsClosed = promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_pool",
			Name:      "connections_closed_total",
			Help:      "Total connections closed in the pool",
		})

		c.poolWaitDuration = promauto.With(registry).NewHistogram(prometheus.HistogramOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_pool",
			Name:      "wait_duration_seconds",
			Help:      "Time spent waiting for a connection from the pool",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
		})
	}

	// Initialize backpressure metrics
	if cfg.EnableBackpressureMetrics {
		c.backpressurePauses = promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_backpressure",
			Name:      "pauses_total",
			Help:      "Total number of backpressure pauses",
		})

		c.backpressureYieldTime = promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: cfg.Namespace,
			Subsystem: cfg.Subsystem + "_backpressure",
			Name:      "yield_time_seconds_total",
			Help:      "Total time spent yielding due to backpressure",
		})
	}

	return c
}

// Connection metrics

// IncConnections increments the total connections counter.
func (c *Collector) IncConnections() {
	c.connectionsTotal.Inc()
}

// IncConnectionsBy increments the total connections counter by n.
func (c *Collector) IncConnectionsBy(n float64) {
	c.connectionsTotal.Add(n)
}

// IncActive increments the active connections gauge.
func (c *Collector) IncActive() {
	c.connectionsActive.Inc()
}

// DecActive decrements the active connections gauge.
func (c *Collector) DecActive() {
	c.connectionsActive.Dec()
}

// SetActive sets the active connections gauge to n.
func (c *Collector) SetActive(n float64) {
	c.connectionsActive.Set(n)
}

// Traffic metrics

// AddBytesSent adds n bytes to the bytes sent counter.
func (c *Collector) AddBytesSent(n float64) {
	c.bytesSent.Add(n)
}

// AddBytesReceived adds n bytes to the bytes received counter.
func (c *Collector) AddBytesReceived(n float64) {
	c.bytesReceived.Add(n)
}

// Error metrics

// IncErrors increments the errors counter.
func (c *Collector) IncErrors() {
	c.errorsTotal.Inc()
}

// IncErrorsBy increments the errors counter by n.
func (c *Collector) IncErrorsBy(n float64) {
	c.errorsTotal.Add(n)
}

// Latency metrics

// ObserveConnectionDuration observes a connection duration.
func (c *Collector) ObserveConnectionDuration(seconds float64) {
	if c.connectionDuration != nil {
		c.connectionDuration.Observe(seconds)
	}
}

// ObserveForwardLatency observes a forward latency.
func (c *Collector) ObserveForwardLatency(seconds float64) {
	if c.forwardLatency != nil {
		c.forwardLatency.Observe(seconds)
	}
}

// Pool metrics

// SetPoolActive sets the active pool connections.
func (c *Collector) SetPoolActive(n float64) {
	if c.poolConnectionsActive != nil {
		c.poolConnectionsActive.Set(n)
	}
}

// IncPoolCreated increments the pool connections created counter.
func (c *Collector) IncPoolCreated() {
	if c.poolConnectionsCreated != nil {
		c.poolConnectionsCreated.Inc()
	}
}

// IncPoolReused increments the pool connections reused counter.
func (c *Collector) IncPoolReused() {
	if c.poolConnectionsReused != nil {
		c.poolConnectionsReused.Inc()
	}
}

// IncPoolClosed increments the pool connections closed counter.
func (c *Collector) IncPoolClosed() {
	if c.poolConnectionsClosed != nil {
		c.poolConnectionsClosed.Inc()
	}
}

// ObservePoolWait observes a pool wait duration.
func (c *Collector) ObservePoolWait(seconds float64) {
	if c.poolWaitDuration != nil {
		c.poolWaitDuration.Observe(seconds)
	}
}

// Backpressure metrics

// IncBackpressurePauses increments the backpressure pauses counter.
func (c *Collector) IncBackpressurePauses() {
	if c.backpressurePauses != nil {
		c.backpressurePauses.Inc()
	}
}

// AddBackpressureYieldTime adds to the backpressure yield time counter.
func (c *Collector) AddBackpressureYieldTime(seconds float64) {
	if c.backpressureYieldTime != nil {
		c.backpressureYieldTime.Add(seconds)
	}
}

// DefaultCollector returns a collector with default configuration.
func DefaultCollector(namespace string) *Collector {
	return NewCollector(Config{
		Namespace:                 namespace,
		EnableConnectionDuration:  true,
		EnableForwardLatency:      true,
		EnablePoolMetrics:         true,
		EnableBackpressureMetrics: true,
	})
}

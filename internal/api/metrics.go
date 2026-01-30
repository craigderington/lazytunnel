package api

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"strconv"
	"time"
)

// Metrics holds all Prometheus metrics for the API
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec

	// Tunnel metrics
	TunnelsTotal  prometheus.Gauge
	TunnelsActive prometheus.Gauge
	TunnelsFailed prometheus.Gauge
	TunnelCreates prometheus.Counter
	TunnelDeletes prometheus.Counter
	TunnelStarts  prometheus.Counter
	TunnelStops   prometheus.Counter
}

// NewMetrics creates and registers all metrics
func NewMetrics() *Metrics {
	return &Metrics{
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "lazytunnel_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "lazytunnel_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "lazytunnel_http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 8),
			},
			[]string{"method", "endpoint"},
		),
		TunnelsTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "lazytunnel_tunnels_total",
				Help: "Total number of tunnels",
			},
		),
		TunnelsActive: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "lazytunnel_tunnels_active",
				Help: "Number of active tunnels",
			},
		),
		TunnelsFailed: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "lazytunnel_tunnels_failed",
				Help: "Number of failed tunnels",
			},
		),
		TunnelCreates: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "lazytunnel_tunnel_creates_total",
				Help: "Total number of tunnel creations",
			},
		),
		TunnelDeletes: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "lazytunnel_tunnel_deletes_total",
				Help: "Total number of tunnel deletions",
			},
		),
		TunnelStarts: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "lazytunnel_tunnel_starts_total",
				Help: "Total number of tunnel starts",
			},
		),
		TunnelStops: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "lazytunnel_tunnel_stops_total",
				Help: "Total number of tunnel stops",
			},
		),
	}
}

// InstrumentHandler wraps an HTTP handler with metrics
func (m *Metrics) InstrumentHandler(endpoint string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(wrapped.statusCode)

		// Record metrics
		m.HTTPRequestsTotal.WithLabelValues(r.Method, endpoint, status).Inc()
		m.HTTPRequestDuration.WithLabelValues(r.Method, endpoint).Observe(duration)
		m.HTTPResponseSize.WithLabelValues(r.Method, endpoint).Observe(float64(wrapped.size))
	}
}

// UpdateTunnelMetrics updates tunnel-related metrics
func (m *Metrics) UpdateTunnelMetrics(total, active, failed int) {
	m.TunnelsTotal.Set(float64(total))
	m.TunnelsActive.Set(float64(active))
	m.TunnelsFailed.Set(float64(failed))
}

// HandleMetrics returns the Prometheus metrics endpoint handler
func HandleMetrics() http.Handler {
	return promhttp.Handler()
}

// responseRecorder wraps http.ResponseWriter to capture status code and size
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

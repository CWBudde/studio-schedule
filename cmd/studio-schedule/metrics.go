package main

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// metricsNamespace prefixes every metric of the app.
const metricsNamespace = "studio_schedule"

// outboxTimeout bounds the outbox count of a scrape.
const outboxTimeout = 5 * time.Second

// promMetrics implements app.Metrics with Prometheus collectors. It lives in
// the server binary only: the web demo has no metrics and must not depend
// on client_golang.
type promMetrics struct {
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	jobRuns         *prometheus.CounterVec
	jobDuration     *prometheus.HistogramVec
	jobLastSuccess  *prometheus.GaugeVec
}

var _ app.Metrics = (*promMetrics)(nil)

// newMetrics registers the app's metrics, the outbox gauge (read from
// unsent at scrape time), the build info and Go's runtime and process
// collectors in a new registry.
func newMetrics(
	version string, unsent func(context.Context) (service.MailCounts, error), logger *slog.Logger,
) (*promMetrics, *prometheus.Registry) {
	m := &promMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Name: "http_requests_total",
			Help: "HTTP requests answered, by method, route pattern and status code.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "http_request_duration_seconds",
			Help:    "Time to answer HTTP requests, by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		jobRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace, Name: "job_runs_total",
			Help: "Runs of background jobs, by job and result (success or failure).",
		}, []string{"job", "result"}),
		jobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace, Name: "job_duration_seconds",
			Help:    "Duration of background job runs, by job.",
			Buckets: prometheus.DefBuckets,
		}, []string{"job"}),
		jobLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace, Name: "job_last_success_timestamp_seconds",
			Help: "Unix time of the last successful run of a background job.",
		}, []string{"job"}),
	}

	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace, Name: "build_info",
		Help:        "Always 1; the version label names the running build.",
		ConstLabels: prometheus.Labels{"version": version},
	})
	buildInfo.Set(1)

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		m.requests, m.requestDuration, m.jobRuns, m.jobDuration, m.jobLastSuccess, buildInfo,
		&outboxCollector{count: unsent, logger: logger},
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m, reg
}

func (m *promMetrics) ObserveRequest(method, route string, status int, d time.Duration) {
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.requestDuration.WithLabelValues(method, route).Observe(d.Seconds())
}

func (m *promMetrics) ObserveJob(name string, err error, d time.Duration) {
	result := "success"
	if err != nil {
		result = "failure"
	} else {
		m.jobLastSuccess.WithLabelValues(name).SetToCurrentTime()
	}

	m.jobRuns.WithLabelValues(name, result).Inc()
	m.jobDuration.WithLabelValues(name).Observe(d.Seconds())
}

// outboxCollector reports the unsent mails of the outbox when it is
// scraped: one cheap query per scrape instead of work on every request.
type outboxCollector struct {
	count  func(context.Context) (service.MailCounts, error)
	logger *slog.Logger
}

var outboxDesc = prometheus.NewDesc(prometheus.BuildFQName(metricsNamespace, "", "outbox_mails"),
	"Mails in the outbox that are not sent, by status: pending (another attempt is due) or failed "+
		"(delivery was given up).", []string{"status"}, nil)

func (c *outboxCollector) Describe(ch chan<- *prometheus.Desc) { ch <- outboxDesc }

// Collect reports nothing if the count fails, so the gauge goes absent
// rather than wrong; the failure is logged.
func (c *outboxCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), outboxTimeout)
	defer cancel()

	counts, err := c.count(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "metrics: counting the outbox failed", slog.Any("err", err))

		return
	}

	ch <- prometheus.MustNewConstMetric(outboxDesc, prometheus.GaugeValue, float64(counts.Pending), "pending")
	ch <- prometheus.MustNewConstMetric(outboxDesc, prometheus.GaugeValue, float64(counts.Failed), "failed")
}

// metricsHandler serves the metrics of reg at /metrics and nothing else.
func metricsHandler(reg *prometheus.Registry, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		Registry: reg, // counts the scrapes that failed
	}))

	return mux
}

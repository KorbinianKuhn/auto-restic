package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	snapshotsTotal *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		snapshotsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "stats",
				Name:      "snapshot_total",
				Help:      "Number of snapshots",
			},
			[]string{"directory"},
		),
	}

	return metrics
}

func (m *Metrics) GetMetricsHandler() http.Handler {

	var r = prometheus.NewRegistry()
	r.MustRegister(
		m.snapshotsTotal,
	)

	handler := promhttp.HandlerFor(r, promhttp.HandlerOpts{})

	return handler
}

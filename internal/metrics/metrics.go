package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	snapshotsTotal     *prometheus.GaugeVec
	snapshotTotalSize  *prometheus.GaugeVec
	snapshotLatestSize *prometheus.GaugeVec
	snapshotLatestTime *prometheus.GaugeVec
	s3Total            *prometheus.GaugeVec
	s3TotalSize        *prometheus.GaugeVec
	s3LatestSize       *prometheus.GaugeVec
	s3LatestTime       *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		snapshotsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "repository",
				Name:      "snapshot_total",
				Help:      "Number of snapshots per backup name",
			},
			[]string{"name"},
		),
		snapshotTotalSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "repository",
				Name:      "snapshot_total_size_bytes",
				Help:      "Total size of all snapshots per backup name",
			},
			[]string{"name"},
		),
		snapshotLatestSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "repository",
				Name:      "snapshot_latest_size_bytes",
				Help:      "Size of the latest snapshot per backup name",
			},
			[]string{"name"},
		),
		snapshotLatestTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "repository",
				Name:      "snapshot_latest_time",
				Help:      "Timestamp of the latest snapshot per backup name",
			},
			[]string{"name"},
		),
		s3Total: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "s3",
				Name:      "total",
				Help:      "Number of snapshot dumps in s3 per backup name",
			},
			[]string{"name"},
		),
		s3TotalSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "s3",
				Name:      "total_size_bytes",
				Help:      "Total size of all snapshot dumps in s3 per backup name",
			},
			[]string{"name"},
		),
		s3LatestSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "s3",
				Name:      "latest_size_bytes",
				Help:      "Size of the latest snapshot dump in s3 per backup name",
			},
			[]string{"name"},
		),
		s3LatestTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "restic",
				Subsystem: "s3",
				Name:      "latest_time",
				Help:      "Timestamp of the latest snapshot dump in s3 per backup name",
			},
			[]string{"name"},
		),
	}

	return metrics
}

func (m *Metrics) SetResticStatsByBackupName(name string, count int, totalSize int64, latestSize int64, latestTime float64) {
	m.snapshotsTotal.WithLabelValues(name).Set(float64(count))
	m.snapshotTotalSize.WithLabelValues(name).Set(float64(totalSize))
	m.snapshotLatestSize.WithLabelValues(name).Set(float64(latestSize))
	m.snapshotLatestTime.WithLabelValues(name).Set(latestTime)
}

func (m *Metrics) SetS3StatsByBackupName(name string, count int, totalSize int64, latestSize int64, latestTime float64) {
	m.s3Total.WithLabelValues(name).Set(float64(count))
	m.s3TotalSize.WithLabelValues(name).Set(float64(totalSize))
	m.s3LatestSize.WithLabelValues(name).Set(float64(latestSize))
	m.s3LatestTime.WithLabelValues(name).Set(latestTime)
}

func (m *Metrics) GetMetricsHandler() http.Handler {

	var r = prometheus.NewRegistry()
	r.MustRegister(
		m.snapshotsTotal,
		m.snapshotTotalSize,
		m.snapshotLatestSize,
		m.snapshotLatestTime,
		m.s3Total,
		m.s3TotalSize,
		m.s3LatestSize,
		m.s3LatestTime,
	)

	handler := promhttp.HandlerFor(r, promhttp.HandlerOpts{})

	return handler
}

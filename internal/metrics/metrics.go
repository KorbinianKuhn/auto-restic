package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type SchedulerError string

const (
	SchedulerErrorResticCheck            SchedulerError = "restic_check"
	SchedulerErrorResticForgetAndPrune   SchedulerError = "restic_forget_and_prune"
	SchedulerErrorResticListSnapshots    SchedulerError = "restic_list_snapshots"
	SchedulerErrorResticGetSnapshotStats SchedulerError = "restic_get_snapshot_stats"
	SchedulerErrorS3ListObjects          SchedulerError = "s3_list_objects"
)

type Metrics struct {
	schedulerErrors                *prometheus.CounterVec
	resticSnapshotErrors           *prometheus.CounterVec
	resticSnapshotLatestDuration   *prometheus.GaugeVec
	resticSnapshotLatestSize       *prometheus.GaugeVec
	resticSnapshotLatestTimestamp  *prometheus.GaugeVec
	resticSnapshotCount            *prometheus.GaugeVec
	resticSnapshotTotalSize        *prometheus.GaugeVec
	s3SnapshotErrors               *prometheus.CounterVec
	s3SnapshotLatestDuration       *prometheus.GaugeVec
	s3SnapshotLatestUploadDuration *prometheus.GaugeVec
	s3SnapshotLatestSize           *prometheus.GaugeVec
	s3SnapshotLatestTimestamp      *prometheus.GaugeVec
	s3SnapshotCount                *prometheus.GaugeVec
	s3SnapshotTotalSize            *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		schedulerErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "backup",
				Subsystem: "scheduler",
				Name:      "errors_total",
				Help:      "Total number of scheduler errors by operation (e.g. restic check, prune, list snapshots)",
			},
			[]string{"operation"},
		),
		resticSnapshotErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "backup",
				Subsystem: "restic",
				Name:      "snapshot_errors_total",
				Help:      "Total number of errors creating restic snapshots per backup name",
			},
			[]string{"backup_name"},
		),
		resticSnapshotLatestDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "restic",
				Name:      "snapshot_latest_duration_seconds",
				Help:      "Duration in seconds of the latest restic snapshot per backup name",
			},
			[]string{"backup_name"},
		),
		resticSnapshotLatestSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "restic",
				Name:      "snapshot_latest_size_bytes",
				Help:      "Size in bytes of the latest restic snapshot per backup name",
			},
			[]string{"backup_name"},
		),
		resticSnapshotLatestTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "restic",
				Name:      "snapshot_latest_timestamp_seconds",
				Help:      "Unix timestamp of the latest restic snapshot per backup name",
			},
			[]string{"backup_name"},
		),
		resticSnapshotCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "restic",
				Name:      "snapshot_count",
				Help:      "Total number of restic snapshots per backup name",
			},
			[]string{"backup_name"},
		),
		resticSnapshotTotalSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "restic",
				Name:      "snapshot_total_size_bytes",
				Help:      "Total size in bytes of all restic snapshots per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_errors_total",
				Help:      "Total number of errors creating or uploading S3 snapshots per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotLatestDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_latest_duration_seconds",
				Help:      "Duration in seconds of the latest S3 snapshot dump per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotLatestUploadDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_latest_upload_duration_seconds",
				Help:      "Duration in seconds of the latest S3 snapshot upload per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotLatestSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_latest_size_bytes",
				Help:      "Size in bytes of the latest S3 snapshot dump per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotLatestTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_latest_timestamp_seconds",
				Help:      "Unix timestamp of the latest S3 snapshot dump per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_count",
				Help:      "Total number of S3 snapshots per backup name",
			},
			[]string{"backup_name"},
		),
		s3SnapshotTotalSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "backup",
				Subsystem: "s3",
				Name:      "snapshot_total_size_bytes",
				Help:      "Total size in bytes of all S3 snapshots per backup name",
			},
			[]string{"backup_name"},
		)}

	metrics.schedulerErrors.WithLabelValues(string(SchedulerErrorResticCheck)).Add(0)
	metrics.schedulerErrors.WithLabelValues(string(SchedulerErrorResticForgetAndPrune)).Add(0)
	metrics.schedulerErrors.WithLabelValues(string(SchedulerErrorResticListSnapshots)).Add(0)
	metrics.schedulerErrors.WithLabelValues(string(SchedulerErrorResticGetSnapshotStats)).Add(0)
	metrics.schedulerErrors.WithLabelValues(string(SchedulerErrorS3ListObjects)).Add(0)

	return metrics
}

func (m *Metrics) AddSchedulerError(operation SchedulerError) {
	m.schedulerErrors.WithLabelValues(string(operation)).Inc()
}

func (m *Metrics) AddResticErrorByBackupName(name string) {
	m.resticSnapshotErrors.WithLabelValues(name).Inc()
}

func (m *Metrics) SetResticStatsByBackupName(name string, count int, totalSize int64, latestSize int64, latestTime float64) {
	m.resticSnapshotCount.WithLabelValues(name).Set(float64(count))
	m.resticSnapshotTotalSize.WithLabelValues(name).Set(float64(totalSize))
	m.resticSnapshotLatestSize.WithLabelValues(name).Set(float64(latestSize))
	m.resticSnapshotLatestTimestamp.WithLabelValues(name).Set(latestTime)
}

func (m *Metrics) SetResticDurationByBackupName(name string, duration float64) {
	m.resticSnapshotLatestDuration.WithLabelValues(name).Set(duration)
}

func (m *Metrics) AddS3ErrorByBackupName(name string) {
	m.s3SnapshotErrors.WithLabelValues(name).Inc()
}

func (m *Metrics) SetS3StatsByBackupName(name string, count int, totalSize int64, latestSize int64, latestTime float64) {
	m.s3SnapshotCount.WithLabelValues(name).Set(float64(count))
	m.s3SnapshotTotalSize.WithLabelValues(name).Set(float64(totalSize))
	m.s3SnapshotLatestSize.WithLabelValues(name).Set(float64(latestSize))
	m.s3SnapshotLatestTimestamp.WithLabelValues(name).Set(latestTime)
}

func (m *Metrics) SetS3DurationByBackupName(name string, duration float64, uploadDuration float64) {
	m.s3SnapshotLatestDuration.WithLabelValues(name).Set(duration)
	m.s3SnapshotLatestUploadDuration.WithLabelValues(name).Set(uploadDuration)
}

func (m *Metrics) GetMetricsHandler() http.Handler {
	var r = prometheus.NewRegistry()
	r.MustRegister(
		m.schedulerErrors,
		m.resticSnapshotErrors,
		m.resticSnapshotCount,
		m.resticSnapshotTotalSize,
		m.resticSnapshotLatestSize,
		m.resticSnapshotLatestTimestamp,
		m.resticSnapshotLatestDuration,
		m.s3SnapshotErrors,
		m.s3SnapshotCount,
		m.s3SnapshotTotalSize,
		m.s3SnapshotLatestSize,
		m.s3SnapshotLatestTimestamp,
		m.s3SnapshotLatestDuration,
		m.s3SnapshotLatestUploadDuration,
	)

	handler := promhttp.HandlerFor(r, promhttp.HandlerOpts{})

	return handler
}

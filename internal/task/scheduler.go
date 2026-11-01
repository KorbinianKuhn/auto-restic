package task

import (
	"log/slog"

	"github.com/go-co-op/gocron/v2"
)

// NewScheduler returns the scheduler for all jobs. It runs one job at a time,
// so jobs triggered via the API wait for a running job instead of running in
// parallel or being dropped.
func NewScheduler() (gocron.Scheduler, error) {
	return gocron.NewScheduler(
		gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait),
		// gocron recovers job panics and reports them only via its logger
		gocron.WithLogger(slog.Default()),
	)
}

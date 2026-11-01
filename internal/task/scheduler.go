package task

import (
	"log/slog"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// stopTimeout is how long Shutdown waits for a running job after cancelling
// its context. It exceeds the time restic gets to release its repository lock
// after SIGINT (1 minute), so Docker's stop_grace_period must exceed it too.
const stopTimeout = 90 * time.Second

// NewScheduler returns the scheduler for all jobs. It runs one job at a time,
// so jobs triggered via the API wait for a running job instead of running in
// parallel or being dropped.
func NewScheduler() (gocron.Scheduler, error) {
	return gocron.NewScheduler(
		gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait),
		gocron.WithStopTimeout(stopTimeout),
		// gocron recovers job panics and reports them only via its logger
		gocron.WithLogger(slog.Default()),
	)
}

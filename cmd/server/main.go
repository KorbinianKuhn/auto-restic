package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/korbiniankuhn/auto-restic/internal/api"
	"github.com/korbiniankuhn/auto-restic/internal/config"
	"github.com/korbiniankuhn/auto-restic/internal/metrics"
	"github.com/korbiniankuhn/auto-restic/internal/restic"
	"github.com/korbiniankuhn/auto-restic/internal/s3"
	"github.com/korbiniankuhn/auto-restic/internal/task"

	"github.com/go-co-op/gocron/v2"
)

func panicOnError(message string, err error) {
	if err != nil {
		slog.Error(message, "error", err)
		panic(err)
	}
}

func main() {
	// Default logger (will be overwritten during config load)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Load config
	c, err := config.Get()
	panicOnError("failed to load config", err)

	// Initialize restic
	r, err := restic.NewRestic(c.Restic.Repository, c.Restic.Password, restic.WithTimeout(c.Restic.Timeout))
	panicOnError("failed to initialize restic", err)
	slog.Info("restic initialized")

	// Initialize S3
	s, err := s3.Get(c.S3.AccessKey, c.S3.SecretKey, c.S3.Endpoint, c.S3.Bucket, c.S3.Secure, s3.WithTimeout(c.S3.Timeout))
	panicOnError("failed to initialize s3", err)
	slog.Info("s3 initialized")

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	slog.Info("health check endpoint", "url", "/health")

	// Initialise metrics
	m := metrics.NewMetrics()
	if c.MetricsEnabled {
		http.Handle("/metrics", m.GetMetricsHandler())
		slog.Info("metrics enabled", "url", "/metrics")
	}

	// Wait group for graceful shutdown
	wg := sync.WaitGroup{}

	// Schedule jobs
	scheduler, err := gocron.NewScheduler(
		gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait),
		// gocron recovers job panics and reports them only via its logger
		gocron.WithLogger(slog.Default()),
	)
	panicOnError("failed to create scheduler", err)

	// Pre backup scripts and restic snapshot creation
	backupJob, err := scheduler.NewJob(
		gocron.CronJob(c.Cron.Backup, true),
		gocron.NewTask(func() {
			task.Backup(c, m, r)
		}),
	)
	panicOnError("failed to schedule backup job", err)

	// restic check
	checkJob, err := scheduler.NewJob(
		gocron.CronJob(c.Cron.Check, true),
		gocron.NewTask(func() {
			task.ResticCheck(m, r)
		}),
	)
	panicOnError("failed to schedule check job", err)

	// restic forget and prune
	pruneJob, err := scheduler.NewJob(
		gocron.CronJob(c.Cron.Prune, true),
		gocron.NewTask(func() {
			task.ForgetAndPrune(c, m, r)
		}),
	)
	panicOnError("failed to schedule prune job", err)

	// S3 backup
	s3Job, err := scheduler.NewJob(
		gocron.CronJob(c.Cron.S3, true),
		gocron.NewTask(func() {
			task.S3Backup(c, m, r, s)
		}),
	)
	panicOnError("failed to schedule s3 job", err)

	// Capture restic and s3 stats at startup and regular intervals
	_, err = scheduler.NewJob(
		gocron.CronJob(c.Cron.Metrics, true),
		gocron.NewTask(func() {
			task.UpdateAllMetrics(c, m, r, s)
		}),
		gocron.JobOption(gocron.WithStartImmediately()),
	)
	panicOnError("failed to schedule metrics job", err)

	// Start scheduler
	scheduler.Start()
	slog.Info("scheduler started")

	// Start http server
	server := http.Server{
		Addr: ":2112",
	}
	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panicOnError("failed to start http server", err)
		}
	}()
	slog.Info("http server started", "port", "2112")

	// API to trigger jobs, on its own listener so it isn't exposed wherever
	// the metrics endpoint is
	var apiServer *http.Server
	if c.API.Listen != "" {
		apiServer = &http.Server{
			Addr: c.API.Listen,
			Handler: api.NewHandler(c.API.Token, map[string]api.Runner{
				"backup": backupJob,
				"s3":     s3Job,
				"check":  checkJob,
				"prune":  pruneJob,
			}),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				panicOnError("failed to start api server", err)
			}
		}()
		slog.Info("api server started", "listen", c.API.Listen)
	}

	// Wait for termination signal
	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, syscall.SIGINT, syscall.SIGTERM)

	<-osSignal
	slog.Info("received termination signal, shutting down")

	// Stop api server first so no new jobs are queued
	if apiServer != nil {
		apiCtx, apiCancel := context.WithTimeout(context.TODO(), 5*time.Second)
		defer apiCancel()
		if err := apiServer.Shutdown(apiCtx); err != nil {
			panicOnError("failed to shutdown api server", err)
		}
	}

	// Stop scheduler
	scheduler.Shutdown()
	slog.Info("scheduler stopped")

	// Stop http server
	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		panicOnError("failed to shutdown http server", err)
	}

	// Run until shutdown is complete
	wg.Wait()
	slog.Info("AutoRestic gracefully stopped")
}

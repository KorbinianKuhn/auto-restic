package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/korbiniankuhn/hetzner-restic/internal/config"
	"github.com/korbiniankuhn/hetzner-restic/internal/metrics"
	"github.com/korbiniankuhn/hetzner-restic/internal/restic"
	"github.com/korbiniankuhn/hetzner-restic/internal/utils"

	"github.com/go-co-op/gocron/v2"
)

func panicOnError(message string, err error) {
	if err != nil {
		slog.Error(message, "error", err)
		panic(err)
	}
}

func main() {
	slog.Info("starting hetzner-restic")

	// Load config
	c, err := config.Get()
	panicOnError("failed to load config", err)
	slog.Info("config loaded")

	// Initialize restic
	r, err := restic.NewRestic(c.ResticRepository, c.ResticPassword)
	panicOnError("failed to initialize restic", err)
	slog.Info("restic initialized")

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
	s, err := gocron.NewScheduler(
		gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait),
	)
	if err != nil {
		panicOnError("failed to create scheduler", err)
	}

	// Pre backup scripts and restic snapshot creation
	s.NewJob(
		gocron.CronJob(c.BackupCron, true),
		gocron.NewTask(func() {
			slog.Info("run pre backup scripts")

			directories, err := utils.GetSubdirectories(c.ResticBackupSources)
			if err != nil {
				slog.Error("failed to get backup directories", "error", err)
				return
			}

			slog.Info("backup directories", "directories", directories)
			for _, dir := range directories {
				err := r.BackupDirectory(dir)
				if err != nil {
					slog.Error("failed to backup directory", "directory", dir, "error", err)
				} else {
					slog.Info("backup completed", "directory", dir)
				}
			}
		}),
	)

	// restic check
	s.NewJob(
		gocron.CronJob(c.CheckCron, true),
		gocron.NewTask(func() {
			slog.Info("run restic check")
			err := r.Check()
			if err != nil {
				slog.Error("failed to check restic repository", "error", err)
			} else {
				slog.Info("restic check completed")
			}
		}),
	)

	// restic forget and prune
	s.NewJob(
		gocron.CronJob(c.PruneCron, true),
		gocron.NewTask(func() {
			slog.Info("run restic forget and prune")
			err := r.ForgetAndPrune(c.ResticKeepDaily, c.ResticKeepWeekly, c.ResticKeepMonthly)
			if err != nil {
				slog.Error("failed to forget and prune restic repository", "error", err)
			} else {
				slog.Info("restic forget and prune completed")
			}
		}),
	)

	// restic forget and prune
	s.NewJob(
		gocron.CronJob(c.S3Cron, true),
		gocron.NewTask(func() {
			slog.Info("run s3 push")
			err := r.ForgetAndPrune(c.ResticKeepDaily, c.ResticKeepWeekly, c.ResticKeepMonthly)
			if err != nil {
				slog.Error("failed to push to s3 bucket", "error", err)
			} else {
				slog.Info("s3 push completed")
			}
		}),
	)

	// S3 backup
	s.NewJob(
		gocron.DurationJob(time.Minute),
		gocron.NewTask(func() {
			slog.Info("s3 backup")
			snapshots, err := r.ListLatestSnapshots()
			if err != nil {
				slog.Error("failed to list latest snapshots", "error", err)
				return
			}
			for _, snapshot := range snapshots {
				archivePath := filepath.Join(c.S3LocalPath, snapshot.Name+".tar.gz")
				err := r.DumpSnapshot(snapshot.ID, archivePath)
				if err != nil {
					slog.Error("failed to archive snapshot", "snapshot", snapshot.Name, "error", err)
					continue
				}
				slog.Info("archived snapshot", "snapshot", snapshot.Name, "archive", archivePath)
			}
		}),
		gocron.JobOption(gocron.WithStartImmediately()),
	)

	// Capture restic stats
	s.NewJob(
		gocron.DurationJob(time.Minute),
		gocron.NewTask(func() {
			slog.Info("capture restic stats")
			snapshots, err := r.ListSnapshots()
			if err != nil {
				slog.Error("failed to list snapshots", "error", err)
				return
			}
			snapshotsCount := map[string]int{}
			for _, snapshot := range snapshots {
				snapshotsCount[snapshot.Paths[0]]++
			}
			slog.Info("snapshots count", "count", snapshotsCount)
		}),
		gocron.JobOption(gocron.WithStartImmediately()),
	)

	// Start scheduler
	s.Start()
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

	// Wait for termination signal
	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, syscall.SIGINT, syscall.SIGTERM)

	<-osSignal
	slog.Info("received termination signal, shutting down")

	// Stop scheduler
	s.Shutdown()
	slog.Info("scheduler stopped")

	// Stop http server
	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		panicOnError("failed to shutdown http server", err)
	}

	// Run until shutdown is complete
	wg.Wait()
	slog.Info("hetzner restic gracefully stopped")
}

package task

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/klauspost/pgzip"
	"github.com/korbiniankuhn/auto-restic/internal/config"
	"github.com/korbiniankuhn/auto-restic/internal/metrics"
	"github.com/korbiniankuhn/auto-restic/internal/restic"
	"github.com/korbiniankuhn/auto-restic/internal/s3"
	"github.com/korbiniankuhn/auto-restic/internal/utils"
)

func ResticCheck(m *metrics.Metrics, r restic.Restic) {
	slog.Info("run restic check")
	err := r.Check()
	if err != nil {
		m.AddSchedulerError(metrics.SchedulerErrorResticCheck)
		slog.Error("failed to check restic repository", "error", err)
	} else {
		slog.Info("restic check completed")
	}
}

func ForgetAndPrune(c config.Config, m *metrics.Metrics, r restic.Restic) {
	slog.Info("run restic forget and prune")
	err := r.ForgetAndPrune(c.Restic.KeepDaily, c.Restic.KeepWeekly, c.Restic.KeepMonthly)
	if err != nil {
		m.AddSchedulerError(metrics.SchedulerErrorResticForgetAndPrune)
		slog.Error("failed to forget and prune restic repository", "error", err)
	} else {
		slog.Info("restic forget and prune completed")
	}

	err = updateResticMetrics(c, m, r)
	if err != nil {
		slog.Error("failed to update restic metrics", "error", err)
	}
}

func Backup(c config.Config, m *metrics.Metrics, r restic.Restic) {
	slog.Info("starting restic backups")
	for _, backup := range c.Backups {
		startedAt := time.Now()
		err := runBackup(backup, func() error {
			return r.BackupDirectory(backup.Name, backup.Path, backup.Exclude, backup.ExcludeFile)
		})
		if err != nil {
			m.AddResticErrorByBackupName(backup.Name)
			slog.Error("failed to backup", "backup", backup.Name, "error", err)
			continue
		}
		slog.Info("finished restic snapshot", "paths", backup.Path)
		duration := time.Since(startedAt)
		m.SetResticDurationByBackupName(backup.Name, duration.Seconds())
	}

	err := updateResticMetrics(c, m, r)
	if err != nil {
		slog.Error("failed to update restic metrics", "error", err)
	}

	slog.Info("restic backups completed")
}

// runBackup runs pre_command, the restic backup and post_command. Once
// pre_command succeeded, post_command always runs, even if the backup failed,
// so pairs like "stop service"/"start service" never leave a service stopped.
func runBackup(backup config.BackupConfig, backupFn func() error) (err error) {
	if backup.PreCommand != "" {
		slog.Info("run pre backup command", "command", backup.PreCommand)
		if err := utils.RunShellCommand(backup.PreCommand, backup.CommandTimeout); err != nil {
			return fmt.Errorf("failed to run pre backup command %q: %w", backup.PreCommand, err)
		}
	}

	if backup.PostCommand != "" {
		defer func() {
			slog.Info("run post backup command", "command", backup.PostCommand)
			if postErr := utils.RunShellCommand(backup.PostCommand, backup.CommandTimeout); postErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to run post backup command %q: %w", backup.PostCommand, postErr))
			}
		}()
	}

	slog.Info("create restic snapshot", "paths", backup.Path)
	if err := backupFn(); err != nil {
		return fmt.Errorf("failed to backup directory %s: %w", backup.Path, err)
	}

	return nil
}

func updateResticMetrics(c config.Config, m *metrics.Metrics, r restic.Restic) error {
	snapshots, err := r.ListSnapshots()
	if err != nil {
		m.AddSchedulerError(metrics.SchedulerErrorResticListSnapshots)
		return fmt.Errorf("failed to list snapshots: %w", err)
	}

	count := map[string]int{}
	totalSize := map[string]int64{}
	latestSize := map[string]int64{}
	latestTime := map[string]float64{}

	for _, backup := range c.Backups {
		count[backup.Name] = 0
		latestSize[backup.Name] = 0
		totalSize[backup.Name] = 0
		latestTime[backup.Name] = 0
	}

	snapshotBackupNames := map[string]bool{}
	for _, snapshot := range snapshots {
		snapshotBackupNames[snapshot.Name] = true
		count[snapshot.Name]++
		latestSize[snapshot.Name] = int64(snapshot.Summary.DataAddedPacked)
		latestTime[snapshot.Name] = float64(snapshot.Time.Unix())
	}

	for name := range snapshotBackupNames {
		stats, err := r.GetSnapshotStatsByName(name)
		if err != nil {
			m.AddSchedulerError(metrics.SchedulerErrorResticGetSnapshotStats)
			return fmt.Errorf("failed to get snapshot stats: %w", err)
		}
		totalSize[name] += int64(stats.TotalSize)
	}

	for name := range count {
		m.SetResticStatsByBackupName(name, count[name], totalSize[name], latestSize[name], float64(latestTime[name]))
	}

	return nil
}

func createAndUploadEncryptedDump(r restic.Restic, s3 *s3.S3, snapshot restic.Snapshot, passphrase string, retries int) error {
	// Create temporary directory to restore snapshot
	tmpDir, err := os.MkdirTemp("", "restic-dump")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Restore snapshot to temporary directory
	slog.Info("restore snapshot to temporary directory", "snapshot", snapshot.Name)
	if err := r.Restore(snapshot.ID, tmpDir); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	// Stream tar.gz.age to S3 with retry
	slog.Info("create encrypted archive and upload to s3", "snapshot", snapshot.Name)

	// Always make at least one attempt, a negative value must not skip the upload
	maxRetries := max(retries, 0) + 1
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Create a pipe for streaming to S3
		pr, pw := io.Pipe()
		errCh := make(chan error, 1)

		go func() {
			err := writeEncryptedArchive(pw, tmpDir, passphrase)
			// Propagate archive errors to the uploader so a broken archive is
			// never completed as a valid object
			pw.CloseWithError(err)
			errCh <- err
		}()

		// Stream directly to S3
		uploadErr := s3.StreamUploadFile(snapshot.Name+".tar.gz.age", pr)

		// The uploader may return early (e.g. network error) without draining
		// the pipe. Closing the reader unblocks the archive writer, otherwise
		// it waits forever and stalls the scheduler.
		pr.Close()

		// Wait for the goroutine to finish and catch errors
		archiveErr := <-errCh

		if uploadErr == nil && archiveErr == nil {
			return nil
		}

		if attempt < maxRetries {
			backoffTime := utils.BackoffTime(attempt)
			slog.Warn("upload or archive creation failed, retrying", "attempt", attempt,
				"backoff_time", backoffTime, "upload_error", uploadErr, "archive_error", archiveErr)
			time.Sleep(backoffTime)
			continue
		}

		if uploadErr != nil {
			return fmt.Errorf("failed to upload to s3 after %d attempts: %w", maxRetries, uploadErr)
		}
		if archiveErr != nil {
			return fmt.Errorf("failed during archive creation after %d attempts: %w", maxRetries, archiveErr)
		}
	}

	return nil
}

func writeEncryptedArchive(w io.Writer, dir, passphrase string) error {
	// Create age recipient
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return fmt.Errorf("failed to create age recipient: %w", err)
	}

	// Wrap writer in age encryptor
	ageWriter, err := age.Encrypt(w, recipient)
	if err != nil {
		return fmt.Errorf("failed to create age encryptor: %w", err)
	}

	// Wrap age in gzip
	gzipWriter := pgzip.NewWriter(ageWriter)

	// Wrap gzip in tar
	tarWriter := tar.NewWriter(gzipWriter)
	err = utils.WriteDirectoryToTar(tarWriter, dir)

	// Close all writers in correct order
	if cerr := tarWriter.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("failed to close tar writer: %w", cerr)
	}
	if cerr := gzipWriter.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("failed to close gzip writer: %w", cerr)
	}
	if cerr := ageWriter.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("failed to close age writer: %w", cerr)
	}

	return err
}

func S3Backup(c config.Config, m *metrics.Metrics, r restic.Restic, s3 *s3.S3) {
	slog.Info("creating s3 backups")

	snapshots, err := r.ListLatestSnapshots()
	if err != nil {
		m.AddSchedulerError(metrics.SchedulerErrorResticListSnapshots)
		slog.Error("failed to list latest snapshots", "error", err)
		return
	}

	for _, backup := range c.Backups {
		startedAt := time.Now()

		snapshot := restic.Snapshot{}
		for _, s := range snapshots {
			if s.Name == backup.Name {
				snapshot = s
				break
			}
		}

		if snapshot.ID == "" {
			m.AddS3ErrorByBackupName(backup.Name)
			slog.Warn("no snapshot found for backup", "backup", backup.Name)
			continue
		}

		retries := c.S3.Retries
		if backup.S3Retries != nil {
			retries = *backup.S3Retries
		}

		err = createAndUploadEncryptedDump(r, s3, snapshot, c.S3.Passphrase, retries)

		if err != nil {
			m.AddS3ErrorByBackupName(backup.Name)
			slog.Error("failed to create and upload snapshot to s3", "snapshot", snapshot.Name, "error", err)
			continue
		}

		m.SetS3DurationByBackupName(backup.Name, time.Since(startedAt).Seconds())
		slog.Info("created and uploaded snapshot to s3", "snapshot", snapshot.Name)
	}

	err = updateS3Metrics(c, m, s3)
	if err != nil {
		slog.Error("failed to update s3 metrics", "error", err)
	}

	slog.Info("s3 backups completed")
}

func updateS3Metrics(c config.Config, m *metrics.Metrics, s *s3.S3) error {
	objects, err := s.ListObjects()
	if err != nil {
		m.AddSchedulerError(metrics.SchedulerErrorS3ListObjects)
		return fmt.Errorf("failed to list s3 objects: %w", err)
	}

	backupNames := make([]string, 0, len(c.Backups))
	for _, backup := range c.Backups {
		backupNames = append(backupNames, backup.Name)
	}

	for name, stats := range aggregateS3Stats(objects, backupNames) {
		m.SetS3StatsByBackupName(name, stats.count, stats.totalSize, stats.latestSize, stats.latestTime)
	}

	return nil
}

type s3Stats struct {
	count      int
	totalSize  int64
	latestSize int64
	latestTime float64
}

// aggregateS3Stats returns the stats for every configured backup name. Delete
// markers, keys that are no backup archive and unconfigured backup names are
// ignored, so the reported label set stays stable.
func aggregateS3Stats(objects []s3.S3Object, backupNames []string) map[string]s3Stats {
	stats := make(map[string]s3Stats, len(backupNames))
	for _, name := range backupNames {
		stats[name] = s3Stats{}
	}

	for _, object := range objects {
		if object.IsDeleteMarker || !strings.HasSuffix(object.Key, ".tar.gz.age") {
			continue
		}
		s, ok := stats[object.BackupName]
		if !ok {
			continue
		}
		s.count++
		s.totalSize += object.Size
		if object.IsLatest {
			s.latestSize = object.Size
			s.latestTime = float64(object.CreatedAt.Unix())
		}
		stats[object.BackupName] = s
	}

	return stats
}

func UpdateAllMetrics(c config.Config, m *metrics.Metrics, r restic.Restic, s *s3.S3) {
	if err := updateAllMetrics(c, m, r, s); err != nil {
		slog.Error("failed to update metrics", "error", err)
	}
}

func updateAllMetrics(c config.Config, m *metrics.Metrics, r restic.Restic, s *s3.S3) error {
	err := updateResticMetrics(c, m, r)
	if err != nil {
		return fmt.Errorf("failed to update restic metrics: %w", err)
	}

	err = updateS3Metrics(c, m, s)
	if err != nil {
		return fmt.Errorf("failed to update s3 metrics: %w", err)
	}

	return nil
}

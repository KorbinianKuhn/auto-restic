package task

import (
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"filippo.io/age"
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
		if backup.PreCommand != "" {
			slog.Info("run pre backup command", "command", backup.PreCommand)
			cmd := exec.Command("sh", "-c", backup.PreCommand)
			err := cmd.Run()
			if err != nil {
				m.AddResticErrorByBackupName(backup.Name)
				slog.Error("failed to run pre backup command", "command", backup.PreCommand, "error", err)
				continue
			}
		}

		slog.Info("create restic snapshot", "paths", backup.Path)
		err := r.BackupDirectory(backup.Name, backup.Path, backup.Exclude, backup.ExcludeFile)
		if err != nil {
			m.AddResticErrorByBackupName(backup.Name)
			slog.Error("failed to backup directory", "directory", backup.Path, "error", err)
			continue
		}

		if backup.PostCommand != "" {
			slog.Info("run post backup command", "command", backup.PostCommand)
			cmd := exec.Command("sh", "-c", backup.PostCommand)
			err := cmd.Run()
			if err != nil {
				m.AddResticErrorByBackupName(backup.Name)
				slog.Error("failed to run post backup command", "command", backup.PostCommand, "error", err)
				continue
			}
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

func createAndUploadEncryptedDump(r restic.Restic, s3 *s3.S3, snapshot restic.Snapshot, passphrase string) error {
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

	// Stream tar.gz.age to S3
	slog.Info("create encrypted archive and upload to s3", "snapshot", snapshot.Name)

	// Create a pipe for streaming to S3
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)

	go func() {
		defer pw.Close()

		// Create age recipient
		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			errCh <- fmt.Errorf("failed to create age recipient: %w", err)
			return
		}

		// Wrap pipe in age encryptor
		ageWriter, err := age.Encrypt(pw, recipient)
		if err != nil {
			errCh <- fmt.Errorf("failed to create age encryptor: %w", err)
			return
		}
		defer ageWriter.Close()

		// Wrap age in gzip
		gzipWriter := gzip.NewWriter(ageWriter)
		defer gzipWriter.Close()

		// Wrap gzip in tar
		err = utils.WriteTar(gzipWriter, tmpDir)

		errCh <- err
	}()

	// Stream directly to S3
	if err := s3.StreamUploadFile(snapshot.Name+".tar.gz.age", pr); err != nil {
		return fmt.Errorf("failed to upload to s3: %w", err)
	}

	// Wait for the goroutine to finish and catch errors
	if err := <-errCh; err != nil {
		return fmt.Errorf("failed during archive creation: %w", err)
	}

	return nil
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

		err = createAndUploadEncryptedDump(r, s3, snapshot, c.S3.Passphrase)

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
	count := map[string]int{}
	totalSize := map[string]int64{}
	latestSize := map[string]int64{}
	latestTime := map[string]float64{}

	for _, backup := range c.Backups {
		count[backup.Name] = 0
		totalSize[backup.Name] = 0
		latestSize[backup.Name] = 0
		latestTime[backup.Name] = 0
	}

	objects, err := s.ListObjects()
	if err != nil {
		m.AddSchedulerError(metrics.SchedulerErrorS3ListObjects)
		return fmt.Errorf("failed to list s3 objects: %w", err)
	}

	for _, object := range objects {
		count[object.BackupName] = 0
		totalSize[object.BackupName] = 0
		latestSize[object.BackupName] = 0
		latestTime[object.BackupName] = 0
	}

	for _, object := range objects {
		count[object.BackupName]++
		totalSize[object.BackupName] += object.Size
		if object.IsLatest {
			latestSize[object.BackupName] = object.Size
			latestTime[object.BackupName] = float64(object.CreatedAt.Unix())
		}
	}

	for name, c := range count {
		m.SetS3StatsByBackupName(name, c, totalSize[name], latestSize[name], latestTime[name])
	}

	return nil
}

func UpdateAllMetrics(c config.Config, m *metrics.Metrics, r restic.Restic, s *s3.S3) error {
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

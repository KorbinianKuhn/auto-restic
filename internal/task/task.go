package task

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"filippo.io/age"
	"github.com/korbiniankuhn/hetzner-restic/internal/config"
	"github.com/korbiniankuhn/hetzner-restic/internal/metrics"
	"github.com/korbiniankuhn/hetzner-restic/internal/restic"
	"github.com/korbiniankuhn/hetzner-restic/internal/s3"
)

func ResticCheck(m *metrics.Metrics, r restic.Restic) {
	slog.Info("run restic check")
	err := r.Check()
	if err != nil {
		slog.Error("failed to check restic repository", "error", err)
	} else {
		slog.Info("restic check completed")
	}
}

func ForgetAndPrune(c config.Config, m *metrics.Metrics, r restic.Restic) {
	slog.Info("run restic forget and prune")
	err := r.ForgetAndPrune(c.Restic.KeepDaily, c.Restic.KeepWeekly, c.Restic.KeepMonthly)
	if err != nil {
		slog.Error("failed to forget and prune restic repository", "error", err)
	} else {
		slog.Info("restic forget and prune completed")
	}

	err = updateResticMetrics(c, m, r)
	if err != nil {
		slog.Error("failed to update restic metrics", "error", err)
	} else {
		slog.Info("restic metrics updated")
	}
}

func Backup(c config.Config, m *metrics.Metrics, r restic.Restic) {
	for _, backup := range c.Backups {
		if backup.PreCommand != "" {
			slog.Info("run pre backup command", "command", backup.PreCommand)
			cmd := exec.Command(backup.PreCommand)
			err := cmd.Run()
			if err != nil {
				slog.Error("failed to run pre backup command", "command", backup.PreCommand, "error", err)
				continue
			}
		}

		slog.Info("create restic snapshot", "paths", backup.Path)
		err := r.BackupDirectory(backup.Name, backup.Path)
		if err != nil {
			slog.Error("failed to backup directory", "directory", backup.Path, "error", err)
			continue
		}

		if backup.PostCommand != "" {
			slog.Info("run post backup command", "command", backup.PostCommand)
			cmd := exec.Command(backup.PostCommand)
			err := cmd.Run()
			if err != nil {
				slog.Error("failed to run post backup command", "command", backup.PostCommand, "error", err)
				continue
			}
		}
	}

	err := updateResticMetrics(c, m, r)
	if err != nil {
		slog.Error("failed to update restic metrics", "error", err)
	} else {
		slog.Info("restic metrics updated")
	}
}

func updateResticMetrics(c config.Config, m *metrics.Metrics, r restic.Restic) error {
	snapshots, err := r.ListSnapshots()
	if err != nil {
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
		latestSize[snapshot.Name] = int64(snapshot.Summary.TotalBytesProcessed)
		totalSize[snapshot.Name] = 0
		latestTime[snapshot.Name] = float64(snapshot.Time.Unix())
	}

	for name := range snapshotBackupNames {
		stats, err := r.GetSnapshotStatsByName(name)
		if err != nil {
			return fmt.Errorf("failed to get snapshot stats: %w", err)
		}
		totalSize[name] = int64(stats.TotalSize)
	}

	for name := range count {
		m.SetResticStatsByBackupName(name, count[name], totalSize[name], latestSize[name], float64(latestTime[name]))
	}

	return nil
}

func createEncryptedDump(r restic.Restic, snapshot, path string) error {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "restic-dump")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()
	slog.Info("tmp dir", "dir", tmpDir)

	// Restore the snapshot to the temporary directory
	err = r.Restore(snapshot, tmpDir)
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	// Create the archive file
	outFile, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	defer outFile.Close()

	// Create age recipient
	recipient, err := age.NewScryptRecipient("secret")
	if err != nil {
		return fmt.Errorf("failed to create recipient: %w", err)
	}

	// Create age writer
	ageWriter, err := age.Encrypt(outFile, recipient)
	if err != nil {
		return fmt.Errorf("failed to create age encryptor: %w", err)
	}
	defer ageWriter.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(ageWriter)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	err = filepath.Walk(tmpDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create a relative path within the archive
		relPath, err := filepath.Rel(tmpDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil // skip root directory itself
		}

		// Prepare tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// For directories, no file content
		if info.Mode().IsDir() {
			return nil
		}

		// For files, write content
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tarWriter, f)
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to walk through files: %w", err)
	}

	return nil
}

func S3Backup(c config.Config, m *metrics.Metrics, r restic.Restic, s *s3.S3) {
	// TODO: Should we use configured backups from config?
	slog.Info("s3 backup")
	snapshots, err := r.ListLatestSnapshots()
	if err != nil {
		slog.Error("failed to list latest snapshots", "error", err)
		return
	}
	for _, snapshot := range snapshots {
		archivePath := filepath.Join(c.S3.LocalPath, snapshot.Name+".tar.gz.age")

		err := createEncryptedDump(r, snapshot.ID, archivePath)

		if err != nil {
			slog.Error("failed to archive snapshot", "snapshot", snapshot.Name, "error", err)
			continue
		}
		slog.Info("archived snapshot", "snapshot", snapshot.Name, "archive", archivePath)
		err = s.UploadFile(archivePath)
		if err != nil {
			slog.Error("failed to upload snapshot to s3", "snapshot", snapshot.Name, "error", err)
			continue
		}
		slog.Info("uploaded snapshot to s3", "snapshot", snapshot.Name, "archive", archivePath)
	}

	err = updateS3Metrics(c, m, s)
	if err != nil {
		slog.Error("failed to update s3 metrics", "error", err)
	} else {
		slog.Info("s3 metrics updated")
	}
}

func updateS3Metrics(c config.Config, m *metrics.Metrics, s *s3.S3) error {
	slog.Info("update s3 metrics")

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

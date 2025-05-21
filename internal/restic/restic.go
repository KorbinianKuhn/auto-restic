package restic

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

type Restic struct {
	repository string
	password   string
}

func NewRestic(path, password string) (Restic, error) {
	r := Restic{
		repository: path,
		password:   password,
	}

	// Check if repository exists
	exists, err := r.exists()
	if err != nil {
		return Restic{}, fmt.Errorf("failed to check if restic repository exists: %w", err)
	}

	// Check if password is correct
	if exists {
		if r.isPasswordCorrect() {
			return r, nil
		} else {
			return Restic{}, fmt.Errorf("restic password is incorrect")
		}
	}

	// Initialize repository if it does not exist
	err = r.init()
	if err != nil {
		return Restic{}, fmt.Errorf("failed to initialize restic repository: %w", err)
	}

	return r, nil
}

func (r Restic) exists() (bool, error) {
	_, err := os.Stat(r.repository)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (r Restic) getCommandEnv() []string {
	env := os.Environ()
	env = append(env, fmt.Sprintf("RESTIC_REPOSITORY=%s", r.repository))
	env = append(env, fmt.Sprintf("RESTIC_PASSWORD=%s", r.password))
	return env
}

func (r Restic) init() error {
	cmd := exec.Command("restic", "init")
	cmd.Env = r.getCommandEnv()

	_, err := cmd.Output()

	if err != nil {
		return fmt.Errorf("failed to initialize restic repository %s: %w", r.repository, err)
	}

	return nil
}

func (r Restic) isPasswordCorrect() bool {
	cmd := exec.Command("restic", "snapshots", "--latest=1", "--no-lock", "--json")
	cmd.Env = r.getCommandEnv()

	_, err := cmd.Output()

	return err == nil
}

func (r Restic) BackupDirectory(name, path string) error {
	cmd := exec.Command("restic", "backup", path, "--tag", fmt.Sprintf("name=%s", name))
	cmd.Env = r.getCommandEnv()

	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("failed to backup directory %s: %w %s", path, err, output)
	}

	return nil
}

func (r Restic) Check() error {
	cmd := exec.Command("restic", "check")
	cmd.Env = r.getCommandEnv()

	_, err := cmd.Output()

	if err != nil {
		return fmt.Errorf("failed to check restic repository: %w", err)
	}

	return nil
}

func (r Restic) ForgetAndPrune(keepDaily, keepWeekly, keepMonthly int) error {
	cmd := exec.Command("restic", "forget", "--prune", fmt.Sprintf("--keep-daily=%d", keepDaily), fmt.Sprintf("--keep-weekly=%d", keepWeekly), fmt.Sprintf("--keep-monthly=%d", keepMonthly))
	cmd.Env = r.getCommandEnv()

	_, err := cmd.Output()

	if err != nil {
		return fmt.Errorf("failed to forget old backups: %w", err)
	}

	return nil
}

type snapshotJson struct {
	Time           time.Time     `json:"time"`
	Tree           string        `json:"tree"`
	Paths          []string      `json:"paths"`
	Hostname       string        `json:"hostname"`
	Username       string        `json:"username"`
	UID            int           `json:"uid"`
	GID            int           `json:"gid"`
	ProgramVersion string        `json:"program_version"`
	ID             string        `json:"id"`
	ShortID        string        `json:"short_id"`
	Tags           []string      `json:"tags"`
	Excludes       []string      `json:"excludes"`
	Summary        backupSummary `json:"summary"`
}

type backupSummary struct {
	FilesNew            int     `json:"files_new"`
	FilesChanged        int     `json:"files_changed"`
	FilesUnmodified     int     `json:"files_unmodified"`
	DirsNew             int     `json:"dirs_new"`
	DirsChanged         int     `json:"dirs_changed"`
	DirsUnmodified      int     `json:"dirs_unmodified"`
	DataBlobs           int     `json:"data_blobs"`
	TreeBlobs           int     `json:"tree_blobs"`
	DataAdded           int     `json:"data_added"`
	TotalFilesProcessed int     `json:"total_files_processed"`
	TotalBytesProcessed int     `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
	SnapshotID          string  `json:"snapshot_id"`
}

type Snapshot struct {
	snapshotJson
	Name string
}

func (s snapshotJson) GetName() string {
	for _, tag := range s.Tags {
		if strings.HasPrefix(tag, "name=") {
			return strings.TrimPrefix(tag, "name=")
		}
	}
	return s.ID
}

func (s snapshotJson) ToInternalSnapshot() Snapshot {
	return Snapshot{
		Name:         s.GetName(),
		snapshotJson: s,
	}
}

func (r Restic) ListSnapshots() ([]Snapshot, error) {
	cmd := exec.Command("restic", "snapshots", "--no-lock", "--json")
	cmd.Env = r.getCommandEnv()

	output, err := cmd.CombinedOutput()

	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w %s", err, output)
	}

	var snapshotJsons []snapshotJson
	err = json.Unmarshal(output, &snapshotJsons)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshots: %w", err)
	}

	snapshots := make([]Snapshot, len(snapshotJsons))
	for i, snapshot := range snapshotJsons {
		snapshots[i] = snapshot.ToInternalSnapshot()
	}

	return snapshots, nil
}

func (r Restic) ListLatestSnapshots() ([]Snapshot, error) {
	cmd := exec.Command("restic", "snapshots", "--latest=1", "--no-lock", "--json")
	cmd.Env = r.getCommandEnv()

	output, err := cmd.CombinedOutput()

	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w %s", err, output)
	}

	var snapshots []snapshotJson
	err = json.Unmarshal(output, &snapshots)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshots: %w", err)
	}

	snapshotList := make([]Snapshot, len(snapshots))
	for i, snapshot := range snapshots {
		snapshotList[i] = snapshot.ToInternalSnapshot()
	}

	return snapshotList, nil
}

type SnapshotStats struct {
	TotalSize              int     `json:"total_size"`
	TotalUncompressedSize  int     `json:"total_uncompressed_size"`
	CompressionRatio       float64 `json:"compression_ratio"`
	CompressionProgress    int     `json:"compression_progress"`
	CompressionSpaceSaving float64 `json:"compression_space_saving"`
	TotalBlobCount         int     `json:"total_blob_count"`
	SnapshotsCount         int     `json:"snapshots_count"`
}

func (r Restic) GetSnapshotStatsByName(name string) (SnapshotStats, error) {
	cmd := exec.Command("restic", "stats", "--json", "--mode", "raw-data", "--no-lock", "--tag", fmt.Sprintf("name=%s", name))
	cmd.Env = r.getCommandEnv()
	output, err := cmd.CombinedOutput()

	if err != nil {
		return SnapshotStats{}, fmt.Errorf("failed to get snapshot stats: %w %s", err, output)
	}

	var stats SnapshotStats
	err = json.Unmarshal(output, &stats)
	if err != nil {
		return SnapshotStats{}, fmt.Errorf("failed to unmarshal snapshots: %w", err)
	}

	return stats, nil
}

func (r Restic) CreateEncryptedDump(snapshot, path string) error {
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
	cmd := exec.Command("restic", "restore", snapshot, "--target", tmpDir, "--no-lock")
	cmd.Env = r.getCommandEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %s %w %s", snapshot, err, output)
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

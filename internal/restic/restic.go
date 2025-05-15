package restic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"time"
)

type Restic struct {
	repository string
	password   string
	mountpoint string
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
	// TODO: optimise for performance (only get latest snapshot)
	cmd := exec.Command("restic", "snapshots")
	cmd.Env = r.getCommandEnv()

	_, err := cmd.Output()

	return err == nil
}

// TODO: tags?
func (r Restic) BackupDirectory(path string) error {
	cmd := exec.Command("restic", "backup", path)
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
	ID   string
	Name string
}

func (r Restic) ListSnapshots() ([]snapshotJson, error) {
	cmd := exec.Command("restic", "snapshots", "--no-lock", "--json")
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
		snapshotList[i] = Snapshot{
			ID:   snapshot.ID,
			Name: path.Base(snapshot.Paths[0]),
		}
	}

	return snapshotList, nil
}

func (r Restic) DumpSnapshot(id, archivePath string) error {
	// Create the archive file
	outFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	defer outFile.Close()

	// Dump the snapshot to the archive file
	cmd := exec.Command("restic", "dump", id, "/", "--target", archivePath, "-a", "tar")
	cmd.Env = r.getCommandEnv()

	output, err := cmd.CombinedOutput()

	if err != nil {
		_ = os.Remove(archivePath)
		return fmt.Errorf("failed to dump snapshot %s: %w %s", id, err, output)
	}

	return nil
}

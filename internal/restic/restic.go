package restic

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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

	cmd := exec.Command("restic", "snapshots", "--latest=1", "--no-lock")
	cmd.Env = r.getCommandEnv()

	output, err := cmd.CombinedOutput()

	if err == nil {
		return r, nil
	}

	if strings.Contains(string(output), "unable to open config file") {
		err = r.init()
		if err != nil {
			return Restic{}, fmt.Errorf("failed to initialize restic repository: %w", err)
		}
	} else if strings.Contains(string(output), "wrong password or no key found") {
		return Restic{}, fmt.Errorf("restic password is incorrect: %w %s", err, output)
	} else {
		return Restic{}, fmt.Errorf("failed to run restic command: %w %s", err, output)
	}

	return r, nil
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

func (r Restic) BackupDirectory(name, path string) error {
	cmd := exec.Command("restic", "backup", path, "--tag", fmt.Sprintf("name=%s", name), "--json")
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
	Parent         string        `json:"parent"`
	Tree           string        `json:"tree"`
	Paths          []string      `json:"paths"`
	Hostname       string        `json:"hostname"`
	Username       string        `json:"username"`
	Tags           []string      `json:"tags"`
	ProgramVersion string        `json:"program_version"`
	Excludes       []string      `json:"excludes"`
	Summary        backupSummary `json:"summary"`
	ID             string        `json:"id"`
	ShortID        string        `json:"short_id"`
}

type backupSummary struct {
	BackupStart         time.Time `json:"backup_start"`
	BackupEnd           time.Time `json:"backup_end"`
	FilesNew            int       `json:"files_new"`
	FilesChanged        int       `json:"files_changed"`
	FilesUnmodified     int       `json:"files_unmodified"`
	DirsNew             int       `json:"dirs_new"`
	DirsChanged         int       `json:"dirs_changed"`
	DirsUnmodified      int       `json:"dirs_unmodified"`
	DataBlobs           int       `json:"data_blobs"`
	TreeBlobs           int       `json:"tree_blobs"`
	DataAdded           int       `json:"data_added"`
	DataAddedPacked     int       `json:"data_added_packed"`
	TotalFilesProcessed int       `json:"total_files_processed"`
	TotalBytesProcessed int       `json:"total_bytes_processed"`
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

func (s snapshotJson) toInternalSnapshot() Snapshot {
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
		snapshots[i] = snapshot.toInternalSnapshot()
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
		snapshotList[i] = snapshot.toInternalSnapshot()
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

func (r Restic) Restore(snapshot, path string) error {
	cmd := exec.Command("restic", "restore", snapshot, "--target", path, "--no-lock")
	cmd.Env = r.getCommandEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %s %w %s", snapshot, err, output)
	}

	return nil
}

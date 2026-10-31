package restic

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/korbiniankuhn/auto-restic/internal/utils"
)

// queryTimeout applies to commands that only read snapshot metadata without
// a lock (snapshots, init). They normally finish within seconds, so 5 minutes
// leaves plenty of headroom for slow backends.
const queryTimeout = 5 * time.Minute

// interruptGracePeriod is how long restic may take to release its repository
// lock after SIGINT before it gets killed
const interruptGracePeriod = time.Minute

// maxStderrSize bounds the stderr output kept for error messages of
// streaming commands
const maxStderrSize = 64 * 1024

type Restic struct {
	repository string
	password   string
	timeout    time.Duration
}

type Option func(*Restic)

// WithTimeout aborts long-running restic commands (backup, restore, check,
// forget --prune, stats) after timeout. 0 disables the timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(r *Restic) {
		r.timeout = timeout
	}
}

func NewRestic(path, password string, opts ...Option) (Restic, error) {
	r := Restic{
		repository: path,
		password:   password,
	}
	for _, opt := range opts {
		opt(&r)
	}

	output, err := r.run(queryTimeout, "snapshots", "--latest=1", "--no-lock")

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

// run executes a restic command with the repository environment and returns
// its combined output. On timeout (0 disables it) restic receives SIGINT so it
// can release its repository lock, and is killed if it does not exit in time.
func (r Restic) run(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := utils.ContextWithOptionalTimeout(timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = r.getCommandEnv()
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = interruptGracePeriod

	output, err := cmd.CombinedOutput()
	return output, utils.WrapTimeout(ctx, "restic "+args[0], timeout, err)
}

func (r Restic) init() error {
	output, err := r.run(queryTimeout, "init")

	if err != nil {
		return fmt.Errorf("failed to initialize restic repository %s: %w %s", r.repository, err, output)
	}

	return nil
}

// hostname is set on every snapshot, otherwise restic uses the hostname of the
// container which changes whenever the container is recreated
const hostname = "auto-restic"

func (r Restic) BackupDirectory(name, path, exclude, excludeFile string) error {
	args := []string{"backup", path, "--host", hostname, "--tag", fmt.Sprintf("name=%s", name), "--json"}
	if exclude != "" {
		args = append(args, "--exclude", exclude)
	}
	if excludeFile != "" {
		args = append(args, "--exclude-file", excludeFile)
	}

	output, err := r.run(r.timeout, args...)

	if err != nil {
		return fmt.Errorf("failed to backup directory %s: %w %s", path, err, output)
	}

	return nil
}

func (r Restic) RemoveBackupDirectory(name string) (string, error) {
	snapshots, err := r.listSnapshotsByName(name)

	if err != nil {
		return "", fmt.Errorf("failed to list snapshots by name: %w", err)
	}

	if len(snapshots) == 0 {
		return "", fmt.Errorf("no snapshots found for name: %s", name)
	}

	ids := []string{}
	for _, s := range snapshots {
		ids = append(ids, s.ShortID)
	}

	args := append([]string{"forget", "--prune", "--json"}, ids...)

	output, err := r.run(r.timeout, args...)

	if err != nil {
		return "", fmt.Errorf("failed to remove backup %s: %w %s", name, err, output)
	}

	return string(output), nil
}

func (r Restic) Check() error {
	output, err := r.run(r.timeout, "check")

	if err != nil {
		return fmt.Errorf("failed to check restic repository: %w %s", err, output)
	}

	return nil
}

// ForgetAndPrune applies the retention policy per backup name. Grouping by tags
// instead of the default host and paths also covers snapshots that were created
// with a different hostname
func (r Restic) ForgetAndPrune(keepDaily, keepWeekly, keepMonthly int) error {
	output, err := r.run(r.timeout, "forget", "--prune", "--group-by", "tags", fmt.Sprintf("--keep-daily=%d", keepDaily), fmt.Sprintf("--keep-weekly=%d", keepWeekly), fmt.Sprintf("--keep-monthly=%d", keepMonthly))

	if err != nil {
		return fmt.Errorf("failed to forget old backups: %w %s", err, output)
	}

	return nil
}

// Unlock removes stale repository locks, e.g. left behind when the container
// was killed during a backup. Locks are stale once restic stopped refreshing
// them for 30 minutes. removeAll also removes locks that are not stale yet,
// so it must only be used when no other restic process uses the repository.
func (r Restic) Unlock(removeAll bool) (string, error) {
	args := []string{"unlock"}
	if removeAll {
		args = append(args, "--remove-all")
	}

	output, err := r.run(queryTimeout, args...)

	if err != nil {
		return "", fmt.Errorf("failed to unlock restic repository: %w %s", err, output)
	}

	return string(output), nil
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
	output, err := r.run(queryTimeout, "snapshots", "--no-lock", "--json")

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

func (r Restic) listSnapshotsByName(name string) ([]Snapshot, error) {
	output, err := r.run(queryTimeout, "snapshots", "--tag", fmt.Sprintf("name=%s", name), "--no-lock", "--json")

	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots by name: %w %s", err, output)
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
	output, err := r.run(queryTimeout, "snapshots", "--latest=1", "--no-lock", "--json")

	if err != nil {
		return nil, fmt.Errorf("failed to list snapshots: %w %s", err, output)
	}

	var snapshots []snapshotJson
	err = json.Unmarshal(output, &snapshots)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshots: %w", err)
	}

	return latestByName(snapshots), nil
}

// latestByName returns the newest snapshot per backup name. restic --latest
// works per host and paths, so it returns one snapshot per hostname
func latestByName(snapshots []snapshotJson) []Snapshot {
	latest := []Snapshot{}
	index := map[string]int{}
	for _, s := range snapshots {
		snapshot := s.toInternalSnapshot()
		i, ok := index[snapshot.Name]
		if !ok {
			index[snapshot.Name] = len(latest)
			latest = append(latest, snapshot)
		} else if snapshot.Time.After(latest[i].Time) {
			latest[i] = snapshot
		}
	}
	return latest
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
	output, err := r.run(r.timeout, "stats", "--json", "--mode", "raw-data", "--no-lock", "--tag", fmt.Sprintf("name=%s", name))

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
	output, err := r.run(r.timeout, "restore", snapshot, "--target", path, "--no-lock")
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %s %w %s", snapshot, err, output)
	}

	return nil
}

// Dump streams the snapshot as tar archive to w. Paths are relative to "/",
// the same layout as a restore of the snapshot. It takes the shared repository
// lock. On timeout restic receives SIGINT so it can release its lock, and is
// killed if it does not exit in time. If writing to w fails, restic is stopped
// and the write error is returned.
func (r Restic) Dump(snapshot string, w io.Writer) error {
	ctx, cancel := utils.ContextWithOptionalTimeout(r.timeout)
	defer cancel()

	stderr := &limitedBuffer{limit: maxStderrSize}
	cmd := exec.CommandContext(ctx, "restic", "dump", snapshot, "/", "--archive", "tar")
	cmd.Env = r.getCommandEnv()
	cmd.Stdout = w
	cmd.Stderr = stderr
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = interruptGracePeriod

	err := utils.WrapTimeout(ctx, "restic dump", r.timeout, cmd.Run())
	if err != nil {
		return fmt.Errorf("failed to dump snapshot %s: %w %s", snapshot, err, stderr)
	}
	return nil
}

// limitedBuffer keeps the first limit bytes written to it and discards the
// rest, so a chatty command can't grow memory without bound
type limitedBuffer struct {
	buf   []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if remaining := b.limit - len(b.buf); remaining > 0 {
		b.buf = append(b.buf, p[:min(len(p), remaining)]...)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

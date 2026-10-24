package restic

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/korbiniankuhn/auto-restic/internal/utils"
)

// fakeRestic answers snapshots immediately, hangs on check until interrupted
// (recording the SIGINT in $MARKER) and fails forget with some output
const fakeRestic = `#!/bin/sh
case "$1" in
  snapshots) echo '[]'; exit 0 ;;
  check)
    trap 'kill $pid; echo interrupted > "$MARKER"; exit 130' INT
    sleep 30 >/dev/null 2>&1 &
    pid=$!
    wait
    exit 0 ;;
  forget) echo "boom"; exit 1 ;;
esac
exit 2
`

func setupFakeRestic(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "restic"), []byte(fakeRestic), 0755); err != nil {
		t.Fatalf("failed to write fake restic: %v", err)
	}
	marker := filepath.Join(dir, "marker")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MARKER", marker)
	return marker
}

func TestTimeoutInterruptsRestic(t *testing.T) {
	marker := setupFakeRestic(t)

	r, err := NewRestic(t.TempDir(), "password", WithTimeout(500*time.Millisecond))
	if err != nil {
		t.Fatalf("failed to create restic: %v", err)
	}

	start := time.Now()
	err = r.Check()
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("expected check to be aborted, took %s", elapsed)
	}

	var timeoutErr *utils.TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if timeoutErr.Operation != "restic check" {
		t.Fatalf("unexpected operation: %s", timeoutErr.Operation)
	}

	content, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(content)) != "interrupted" {
		t.Fatalf("expected restic to receive SIGINT, marker: %q, error: %v", content, err)
	}
}

func TestErrorIncludesOutput(t *testing.T) {
	setupFakeRestic(t)

	r, err := NewRestic(t.TempDir(), "password")
	if err != nil {
		t.Fatalf("failed to create restic: %v", err)
	}

	err = r.ForgetAndPrune(7, 4, 3)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected command output in error, got %v", err)
	}
	var timeoutErr *utils.TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("expected no timeout error, got %v", err)
	}
}

func TestLatestByNamePicksNewestSnapshot(t *testing.T) {
	now := time.Now()
	snapshots := []snapshotJson{
		{ID: "old-host", Time: now.Add(-48 * time.Hour), Tags: []string{"name=data"}},
		{ID: "other", Time: now.Add(-24 * time.Hour), Tags: []string{"name=other"}},
		{ID: "new-host", Time: now, Tags: []string{"name=data"}},
	}

	latest := latestByName(snapshots)

	if len(latest) != 2 {
		t.Fatalf("expected one snapshot per name, got %d", len(latest))
	}
	if latest[0].Name != "data" || latest[0].ID != "new-host" {
		t.Fatalf("expected newest snapshot for data, got %s/%s", latest[0].Name, latest[0].ID)
	}
	if latest[1].Name != "other" || latest[1].ID != "other" {
		t.Fatalf("expected snapshot for other, got %s/%s", latest[1].Name, latest[1].ID)
	}
}

// TestRetentionIgnoresHostname simulates snapshots of a recreated container
// which had a different hostname before
func TestRetentionIgnoresHostname(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "file"), []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	r, err := NewRestic(t.TempDir(), "password")
	if err != nil {
		t.Fatalf("failed to create restic: %v", err)
	}

	if output, err := r.run(queryTimeout, "backup", src, "--host", "3f2a1b0c9d8e", "--tag", "name=data"); err != nil {
		t.Fatalf("failed to create old snapshot: %v %s", err, output)
	}
	if err := r.BackupDirectory("data", src, "", ""); err != nil {
		t.Fatalf("failed to backup: %v", err)
	}

	latest, err := r.ListLatestSnapshots()
	if err != nil {
		t.Fatalf("failed to list latest snapshots: %v", err)
	}
	if len(latest) != 1 || latest[0].Hostname != hostname {
		t.Fatalf("expected the newest snapshot only, got %+v", latest)
	}

	if err := r.ForgetAndPrune(1, 0, 0); err != nil {
		t.Fatalf("failed to forget and prune: %v", err)
	}

	snapshots, err := r.ListSnapshots()
	if err != nil {
		t.Fatalf("failed to list snapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Hostname != hostname {
		t.Fatalf("expected one snapshot with hostname %s after prune, got %+v", hostname, snapshots)
	}
}

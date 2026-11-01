package task

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/korbiniankuhn/auto-restic/internal/config"
	"github.com/korbiniankuhn/auto-restic/internal/metrics"
	"github.com/korbiniankuhn/auto-restic/internal/restic"
)

func TestRunBackupRunsPostCommandOnBackupFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "post")
	backup := config.BackupConfig{
		Name:        "data",
		Path:        "/data",
		PreCommand:  "true",
		PostCommand: "touch " + marker,
	}

	err := runBackup(t.Context(), backup, func() error { return errors.New("restic failed") })
	if err == nil || !strings.Contains(err.Error(), "restic failed") {
		t.Fatalf("expected backup error, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("expected post command to run after failed backup: %v", statErr)
	}
}

func TestRunBackupJoinsBackupAndPostCommandErrors(t *testing.T) {
	backup := config.BackupConfig{Name: "data", Path: "/data", PostCommand: "exit 3"}

	err := runBackup(t.Context(), backup, func() error { return errors.New("restic failed") })
	if err == nil || !strings.Contains(err.Error(), "restic failed") || !strings.Contains(err.Error(), "post backup command") {
		t.Fatalf("expected backup and post command errors, got %v", err)
	}
}

func TestRunBackupSkipsBackupAndPostCommandOnPreCommandFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "post")
	backup := config.BackupConfig{
		Name:        "data",
		Path:        "/data",
		PreCommand:  "exit 1",
		PostCommand: "touch " + marker,
	}

	called := false
	err := runBackup(t.Context(), backup, func() error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "pre backup command") {
		t.Fatalf("expected pre command error, got %v", err)
	}
	if called {
		t.Fatal("expected backup to be skipped")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("expected post command to be skipped, stat: %v", statErr)
	}
}

func TestRunBackupSuccess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "post")
	backup := config.BackupConfig{Name: "data", Path: "/data", PreCommand: "true", PostCommand: "touch " + marker}

	if err := runBackup(t.Context(), backup, func() error { return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected post command to run: %v", err)
	}
}

func TestRunBackupRunsPostCommandOnShutdown(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "post")
	backup := config.BackupConfig{Name: "data", Path: "/data", PreCommand: "true", PostCommand: "touch " + marker}

	ctx, cancel := context.WithCancel(t.Context())
	err := runBackup(ctx, backup, func() error {
		cancel()
		return ctx.Err()
	})
	if err == nil {
		t.Fatal("expected backup error")
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("expected post command to run after shutdown: %v", statErr)
	}
}

// fakeBackupRestic fails the backup of /fail and hangs on the backup of /hang
// until interrupted
const fakeBackupRestic = `#!/bin/sh
case "$1 $2" in
  "snapshots"*) echo '[]'; exit 0 ;;
  "backup /fail") echo "boom"; exit 1 ;;
  "backup /hang")
    trap 'kill $pid; exit 130' INT
    sleep 30 >/dev/null 2>&1 &
    pid=$!
    wait
    exit 0 ;;
esac
exit 2
`

func TestBackupInterruptedByShutdownIsNoFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "restic"), []byte(fakeBackupRestic), 0755); err != nil {
		t.Fatalf("failed to write fake restic: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	r, err := restic.NewRestic(t.TempDir(), "password")
	if err != nil {
		t.Fatalf("failed to create restic: %v", err)
	}
	m := metrics.NewMetrics()

	// A real failure is counted
	Backup(t.Context(), config.Config{Backups: []config.BackupConfig{{Name: "failed", Path: "/fail"}}}, m, r)

	// An interrupted backup isn't, and the following backups don't start
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(500*time.Millisecond, cancel)
	c := config.Config{Backups: []config.BackupConfig{
		{Name: "interrupted", Path: "/hang"},
		{Name: "skipped", Path: "/fail"},
	}}
	Backup(ctx, c, m, r)

	rec := httptest.NewRecorder()
	m.GetMetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)

	if !strings.Contains(string(body), `snapshot_errors_total{backup_name="failed"} 1`) {
		t.Fatalf("expected the failed backup to be counted, got:\n%s", body)
	}
	for _, name := range []string{"interrupted", "skipped"} {
		if strings.Contains(string(body), `backup_name="`+name+`"`) {
			t.Fatalf("expected no metrics for %s, got:\n%s", name, body)
		}
	}
}

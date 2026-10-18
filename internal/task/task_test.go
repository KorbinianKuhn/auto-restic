package task

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/korbiniankuhn/auto-restic/internal/config"
)

func TestRunBackupRunsPostCommandOnBackupFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "post")
	backup := config.BackupConfig{
		Name:        "data",
		Path:        "/data",
		PreCommand:  "true",
		PostCommand: "touch " + marker,
	}

	err := runBackup(backup, func() error { return errors.New("restic failed") })
	if err == nil || !strings.Contains(err.Error(), "restic failed") {
		t.Fatalf("expected backup error, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("expected post command to run after failed backup: %v", statErr)
	}
}

func TestRunBackupJoinsBackupAndPostCommandErrors(t *testing.T) {
	backup := config.BackupConfig{Name: "data", Path: "/data", PostCommand: "exit 3"}

	err := runBackup(backup, func() error { return errors.New("restic failed") })
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
	err := runBackup(backup, func() error { called = true; return nil })
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

	if err := runBackup(backup, func() error { return nil }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected post command to run: %v", err)
	}
}

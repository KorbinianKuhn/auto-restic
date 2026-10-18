package restic

import (
	"errors"
	"os"
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

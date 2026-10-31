package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	runs int
	err  error
}

func (f *fakeRunner) RunNow() error {
	f.runs++
	return f.err
}

// startServer serves runners on a socket in a short temp dir, since unix
// socket paths are limited to about 100 characters
func startServer(t *testing.T, runners map[string]Runner) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ar")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "api.sock")

	server, err := NewServer(socket, runners)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve() }()
	t.Cleanup(func() {
		server.Shutdown(context.Background())
		if err := <-done; err != nil {
			t.Errorf("serve failed: %v", err)
		}
	})
	return socket
}

func TestTrigger(t *testing.T) {
	backup := &fakeRunner{}
	failing := &fakeRunner{err: errors.New("queue full")}
	socket := startServer(t, map[string]Runner{"backup": backup, "prune": failing})

	if err := Trigger(socket, "backup"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backup.runs != 1 {
		t.Fatalf("expected backup to be queued once, got %d", backup.runs)
	}

	if err := Trigger(socket, "unknown"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected unknown job error, got %v", err)
	}
	if err := Trigger(socket, "prune"); err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("expected run error, got %v", err)
	}
}

func TestSocketPermissions(t *testing.T) {
	socket := startServer(t, nil)
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected socket mode 0600, got %o", perm)
	}
}

func TestStaleSocketIsReplaced(t *testing.T) {
	dir, err := os.MkdirTemp("", "ar")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "api.sock")
	if err := os.WriteFile(socket, nil, 0600); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(socket, nil)
	if err != nil {
		t.Fatalf("expected stale socket to be replaced, got %v", err)
	}
	server.listener.Close()
}

func TestSecondServerIsRejected(t *testing.T) {
	socket := startServer(t, nil)
	if _, err := NewServer(socket, nil); err == nil || !strings.Contains(err.Error(), "another server") {
		t.Fatalf("expected running server to be detected, got %v", err)
	}
}

func TestTriggerWithoutServer(t *testing.T) {
	err := Trigger(filepath.Join(t.TempDir(), "missing.sock"), "backup")
	if err == nil || !strings.Contains(err.Error(), "is it running") {
		t.Fatalf("expected a hint that the server isn't running, got %v", err)
	}
}

func TestOnlyPostIsAllowed(t *testing.T) {
	backup := &fakeRunner{}
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/backup/run", nil)
	rec := httptest.NewRecorder()
	newHandler(map[string]Runner{"backup": backup}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed || backup.runs != 0 {
		t.Fatalf("expected 405 without a run, got %d and %d runs", rec.Code, backup.runs)
	}
}

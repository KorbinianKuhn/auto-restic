package utils

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunShellCommand(t *testing.T) {
	if err := RunShellCommand(t.Context(), "true", 0); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	err := RunShellCommand(t.Context(), "echo boom; exit 3", 0)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected exit status and output in error, got %v", err)
	}
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("expected no timeout error, got %v", err)
	}
}

func TestRunShellCommandTimeoutKillsChildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	start := time.Now()
	err := RunShellCommand(t.Context(), "sleep 30 & echo $! > "+pidFile+"; wait", 500*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("expected command to be aborted, took %s", elapsed)
	}

	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if timeoutErr.Operation != "shell command" || timeoutErr.Timeout != 500*time.Millisecond {
		t.Fatalf("unexpected timeout error: %+v", timeoutErr)
	}

	content, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("invalid pid: %v", err)
	}

	// The orphaned child is reaped asynchronously
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child process %d is still running", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunShellCommandCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(500*time.Millisecond, cancel)

	start := time.Now()
	err := RunShellCommand(ctx, "sleep 30", 0)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("expected command to be aborted, took %s", elapsed)
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("expected no timeout error, got %v", err)
	}
}

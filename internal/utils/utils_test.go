package utils

import (
	"archive/tar"
	"bytes"
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
	if err := RunShellCommand("true", 0); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	err := RunShellCommand("echo boom; exit 3", 0)
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
	err := RunShellCommand("sleep 30 & echo $! > "+pidFile+"; wait", 500*time.Millisecond)
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

type tarEntry struct {
	name     string
	typeflag byte
	linkname string
	body     string
	mode     int64
}

func buildTar(t *testing.T, entries []tarEntry) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	w := tar.NewWriter(buf)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0644
		}
		header := &tar.Header{Name: e.name, Typeflag: e.typeflag, Linkname: e.linkname, Mode: mode, Size: int64(len(e.body))}
		if err := w.WriteHeader(header); err != nil {
			t.Fatalf("failed to write header: %v", err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("failed to write body: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	return buf
}

func TestExtractTar(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest")
	archive := buildTar(t, []tarEntry{
		{name: "dir", typeflag: tar.TypeDir, mode: 0755},
		// Read-only file in a directory without its own entry
		{name: "nested/file.txt", typeflag: tar.TypeReg, body: "hello", mode: 0444},
		{name: "nested/other.txt", typeflag: tar.TypeReg, body: "world"},
		{name: "dir/link", typeflag: tar.TypeSymlink, linkname: "../nested/file.txt"},
		// Symlinks are restored as-is, even if they point outside of dest
		{name: "localtime", typeflag: tar.TypeSymlink, linkname: "/etc/localtime"},
		{name: "dir/up", typeflag: tar.TypeSymlink, linkname: "../../outside"},
	})

	if err := ExtractTar(archive, dest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dest, "dir", "link"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("expected symlinked content, got %q (%v)", content, err)
	}
	content, err = os.ReadFile(filepath.Join(dest, "nested", "other.txt"))
	if err != nil || string(content) != "world" {
		t.Fatalf("expected file content, got %q (%v)", content, err)
	}
	for link, want := range map[string]string{"localtime": "/etc/localtime", "dir/up": "../../outside"} {
		got, err := os.Readlink(filepath.Join(dest, link))
		if err != nil || got != want {
			t.Fatalf("expected symlink %s -> %s, got %q (%v)", link, want, got, err)
		}
	}
}

func TestExtractTarRejectsEscapes(t *testing.T) {
	tests := map[string][]tarEntry{
		"parent traversal": {{name: "../evil.txt", typeflag: tar.TypeReg, body: "x"}},
		"nested traversal": {{name: "a/../../evil.txt", typeflag: tar.TypeReg, body: "x"}},
		"absolute path":    {{name: "/tmp/evil.txt", typeflag: tar.TypeReg, body: "x"}},
		"write through symlink": {
			{name: "link", typeflag: tar.TypeSymlink, linkname: ".."},
			{name: "link/evil.txt", typeflag: tar.TypeReg, body: "x"},
		},
		"write through absolute symlink": {
			{name: "a/link", typeflag: tar.TypeSymlink, linkname: "{root}"},
			{name: "a/link/evil.txt", typeflag: tar.TypeReg, body: "x"},
		},
		"directory through symlink": {
			{name: "link", typeflag: tar.TypeSymlink, linkname: "{root}"},
			{name: "link/sub", typeflag: tar.TypeDir, mode: 0755},
		},
		"overwrite symlink target": {
			{name: "link", typeflag: tar.TypeSymlink, linkname: "{root}/evil.txt"},
			{name: "link", typeflag: tar.TypeReg, body: "x"},
		},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "dest")
			for i := range entries {
				entries[i].linkname = strings.ReplaceAll(entries[i].linkname, "{root}", root)
			}
			err := ExtractTar(buildTar(t, entries), dest)
			if err == nil || !strings.Contains(err.Error(), "illegal") {
				t.Fatalf("expected illegal path error, got %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "evil.txt")); !os.IsNotExist(err) {
				t.Fatalf("file was written outside of destination")
			}
			if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
				t.Fatalf("directory was created outside of destination")
			}
		})
	}
}

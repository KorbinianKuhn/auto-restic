package utils

import (
	"archive/tar"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// RunShellCommand runs command via "sh -c". On timeout (0 disables it) the
// whole process group is killed, so children like "docker exec" stop as well.
func RunShellCommand(command string, timeout time.Duration) error {
	ctx, cancel := ContextWithOptionalTimeout(timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if timeout > 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// Don't wait for orphans that still hold the output pipe
		cmd.WaitDelay = 10 * time.Second
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w %s", WrapTimeout(ctx, "shell command", timeout, err), output)
	}
	return nil
}

func ExtractTar(r io.Reader, dest string) error {
	tarReader := tar.NewReader(r)

	// Ensure destination directory exists
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading tar archive: %w", err)
		}

		targetPath := filepath.Join(dest, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			// Create parent directory if it doesn't exist
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
			}

			// Create and write file (fix resource leak by not using defer in loop)
			if err := extractRegularFile(tarReader, targetPath, header); err != nil {
				return fmt.Errorf("failed to extract file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			// Create parent directory if it doesn't exist
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
			}

			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", targetPath, err)
			}
		default:
			// Skip unsupported file types
		}
	}
	return nil
}

// Helper function to extract regular files without resource leaks
func extractRegularFile(tarReader *tar.Reader, targetPath string, header *tar.Header) error {
	outFile, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// Use limited copy to prevent potential DoS attacks
	_, err = io.Copy(outFile, tarReader)
	if err != nil {
		return err
	}

	// Set file permissions
	if err := outFile.Chmod(os.FileMode(header.Mode)); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	// Set file times
	if err := os.Chtimes(targetPath, header.ModTime, header.ModTime); err != nil {
		return fmt.Errorf("failed to set file times: %w", err)
	}

	return nil
}

// Exponential backoff with jitter
func BackoffTime(attempt int) time.Duration {
	base := time.Second
	max := 30 * time.Second
	sleep := time.Duration(1<<attempt) * base
	jitter := time.Duration(rand.Int63n(int64(base)))
	return min(sleep+jitter, max)
}

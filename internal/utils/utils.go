package utils

import (
	"archive/tar"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	dest = filepath.Clean(dest)

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

		// Reject entries that would be written outside of dest
		if filepath.IsAbs(header.Name) {
			return fmt.Errorf("illegal absolute path in archive: %s", header.Name)
		}
		targetPath := filepath.Join(dest, header.Name)
		if !isWithin(dest, targetPath) {
			return fmt.Errorf("illegal path outside of destination in archive: %s", header.Name)
		}
		// Symlinks are restored as-is and may point anywhere, so never write
		// an entry through one
		if err := checkNoSymlink(dest, targetPath); err != nil {
			return fmt.Errorf("illegal path in archive: %s: %w", header.Name, err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			// Create parent directory if it doesn't exist
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
			}

			// Create and write file (fix resource leak by not using defer in loop)
			if err := extractRegularFile(tarReader, targetPath, header); err != nil {
				return fmt.Errorf("failed to extract file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			// Create parent directory if it doesn't exist
			parentDir := filepath.Dir(targetPath)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
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

// checkNoSymlink returns an error if target or any existing path component
// between dest and target is a symlink
func checkNoSymlink(dest, target string) error {
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == "." {
		return err
	}
	path := dest
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", path)
		}
	}
	return nil
}

// isWithin reports whether the cleaned path is dest or inside of it
func isWithin(dest, path string) bool {
	rel, err := filepath.Rel(dest, filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

func WriteDirectoryToTar(w *tar.Writer, src string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking path %s: %w", path, err)
		}

		// Calculate relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		if relPath == "." {
			return nil // Skip root directory
		}

		// Handle symbolic links
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return fmt.Errorf("failed to create tar header for %s: %w", path, err)
		}
		header.Name = relPath

		header.ModTime = info.ModTime()
		// Set ownership and filetime information if available
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			header.Uid = int(stat.Uid)
			header.Gid = int(stat.Gid)
		}

		// Write header
		if err := w.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", path, err)
		}

		// Write file content for regular files
		if info.Mode().IsRegular() {
			if err := writeFileToTar(w, path); err != nil {
				return fmt.Errorf("failed to write file content for %s: %w", path, err)
			}
		}

		return nil
	})
}

// Helper function to write regular file content to tar with better error handling
func writeFileToTar(tarWriter *tar.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(tarWriter, file)
	return err
}

// Exponential backoff with jitter
func BackoffTime(attempt int) time.Duration {
	base := time.Second
	max := 30 * time.Second
	sleep := time.Duration(1<<attempt) * base
	jitter := time.Duration(rand.Int63n(int64(base)))
	return min(sleep+jitter, max)
}

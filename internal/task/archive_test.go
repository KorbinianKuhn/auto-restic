package task

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/klauspost/pgzip"
	"github.com/korbiniankuhn/auto-restic/internal/restic"
	"github.com/korbiniankuhn/auto-restic/internal/utils"
)

// The server uses a scrypt recipient, X25519 keeps the test fast
func testIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestWriteEncryptedArchiveRoundtrip(t *testing.T) {
	identity := testIdentity(t)
	encrypted := &bytes.Buffer{}
	err := writeEncryptedArchive(encrypted, identity.Recipient(), func(w io.Writer) error {
		_, err := w.Write([]byte("tar-data"))
		return err
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	decrypted, err := age.Decrypt(encrypted, identity)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}
	gz, err := pgzip.NewReader(decrypted)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}
	content, err := io.ReadAll(gz)
	if err != nil || string(content) != "tar-data" {
		t.Fatalf("expected original content, got %q (%v)", content, err)
	}
}

func TestWriteEncryptedArchiveReturnsTarError(t *testing.T) {
	err := writeEncryptedArchive(io.Discard, testIdentity(t).Recipient(), func(w io.Writer) error {
		return errors.New("dump failed")
	})
	if err == nil || err.Error() != "dump failed" {
		t.Fatalf("expected tar error, got %v", err)
	}
}

// The streamed dump must stay restorable with "auto-restic s3 restore". Runs
// against a real restic binary if installed.
func TestDumpArchiveRestores(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	src := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(src, "db"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "db", "dump.archive"), []byte("mongo"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dump.archive", filepath.Join(src, "db", "latest")); err != nil {
		t.Fatal(err)
	}

	r, err := restic.NewRestic(filepath.Join(t.TempDir(), "repo"), "password")
	if err != nil {
		t.Fatalf("failed to create restic: %v", err)
	}
	if err := r.BackupDirectory("data", src, "", ""); err != nil {
		t.Fatalf("failed to backup: %v", err)
	}

	identity := testIdentity(t)
	encrypted := &bytes.Buffer{}
	err = writeEncryptedArchive(encrypted, identity.Recipient(), func(w io.Writer) error {
		return r.Dump("latest", w)
	})
	if err != nil {
		t.Fatalf("failed to create archive: %v", err)
	}

	// Same steps as "auto-restic s3 restore"
	decrypted, err := age.Decrypt(encrypted, identity)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}
	gz, err := pgzip.NewReader(decrypted)
	if err != nil {
		t.Fatalf("failed to decompress: %v", err)
	}
	dest := t.TempDir()
	if err := utils.ExtractTar(gz, dest); err != nil {
		t.Fatalf("failed to extract: %v", err)
	}

	restored := filepath.Join(dest, src)
	content, err := os.ReadFile(filepath.Join(restored, "db", "latest"))
	if err != nil || string(content) != "mongo" {
		t.Fatalf("expected restored file through symlink, got %q (%v)", content, err)
	}
	info, err := os.Stat(filepath.Join(restored, "db", "dump.archive"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("expected mode 0600, got %v (%v)", info, err)
	}
}

package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// setup changes into an empty directory with the given config.yml and sets
// all required environment variables
func setup(t *testing.T, configYaml string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("config.yml", []byte(configYaml), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	for _, key := range []string{"RESTIC_PASSWORD", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_ENDPOINT", "S3_BUCKET", "S3_PASSPHRASE"} {
		t.Setenv(key, "test")
	}
}

func TestTimeoutsDisabledByDefault(t *testing.T) {
	setup(t, "backups:\n  - name: data\n    path: /data\n")

	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Restic.Timeout != 0 || c.S3.Timeout != 0 || c.Backups[0].CommandTimeout != 0 {
		t.Fatalf("expected no timeouts, got restic=%s s3=%s command=%s", c.Restic.Timeout, c.S3.Timeout, c.Backups[0].CommandTimeout)
	}
}

func TestTimeoutsFromConfigAndEnv(t *testing.T) {
	setup(t, `
s3:
  timeout: 2h
backups:
  - name: data
    path: /data
    command_timeout: 30m
`)
	t.Setenv("RESTIC_TIMEOUT", "90m")

	c, err := Get()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Restic.Timeout != 90*time.Minute {
		t.Errorf("restic timeout: got %s", c.Restic.Timeout)
	}
	if c.S3.Timeout != 2*time.Hour {
		t.Errorf("s3 timeout: got %s", c.S3.Timeout)
	}
	if c.Backups[0].CommandTimeout != 30*time.Minute {
		t.Errorf("command timeout: got %s", c.Backups[0].CommandTimeout)
	}
}

func TestInvalidTimeouts(t *testing.T) {
	tests := map[string]string{
		"negative":       "s3:\n  timeout: -1m\n",
		"missing unit":   "restic:\n  timeout: 3600\n",
		"backup command": "backups:\n  - name: data\n    path: /data\n    command_timeout: -5s\n",
	}
	for name, configYaml := range tests {
		t.Run(name, func(t *testing.T) {
			setup(t, configYaml)
			_, err := Get()
			if err == nil || !strings.Contains(err.Error(), "timeout") {
				t.Fatalf("expected timeout validation error, got %v", err)
			}
		})
	}
}

func TestNegativeS3Retries(t *testing.T) {
	setup(t, "s3:\n  retries: -1\n")
	_, err := Get()
	if err == nil || !strings.Contains(err.Error(), "s3.retries") {
		t.Fatalf("expected retries validation error, got %v", err)
	}
	t.Setenv("S3_RETRIES", "0")
	if _, err := Get(); err != nil {
		t.Fatalf("expected zero retries to be valid, got %v", err)
	}
}

func TestValidateRetries(t *testing.T) {
	if err := validateRetries("backups[data].s3_retries", -1); err == nil || !strings.Contains(err.Error(), "backups[data].s3_retries") {
		t.Fatalf("expected validation error, got %v", err)
	}
	for _, retries := range []int{0, 3} {
		if err := validateRetries("s3.retries", retries); err != nil {
			t.Fatalf("expected %d to be valid, got %v", retries, err)
		}
	}
}

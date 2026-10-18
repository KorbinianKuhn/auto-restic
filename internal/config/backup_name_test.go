package config

import (
	"strings"
	"testing"
)

func TestValidBackupNames(t *testing.T) {
	setup(t, "backups:\n  - name: mongodb-dump_v1.2\n    path: /data\n")
	if _, err := Get(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvalidBackupNames(t *testing.T) {
	tests := map[string]string{
		"missing": "backups:\n  - path: /data\n",
		"empty":   "backups:\n  - name: \"\"\n    path: /data\n",
		"comma":   "backups:\n  - name: a,b\n    path: /data\n",
		"slash":   "backups:\n  - name: a/b\n    path: /data\n",
		"space":   "backups:\n  - name: a b\n    path: /data\n",
	}
	for name, configYaml := range tests {
		t.Run(name, func(t *testing.T) {
			setup(t, configYaml)
			_, err := Get()
			if err == nil || !strings.Contains(err.Error(), "invalid backup name") {
				t.Fatalf("expected backup name validation error, got %v", err)
			}
		})
	}
}

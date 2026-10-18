package task

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/korbiniankuhn/auto-restic/internal/config"
	"github.com/korbiniankuhn/auto-restic/internal/metrics"
	"github.com/korbiniankuhn/auto-restic/internal/restic"
)

// fakeRestic lists one snapshot each for "ok" and "broken", and fails the
// stats call for "broken"
const fakeRestic = `#!/bin/sh
case "$*" in
  snapshots*)
    echo '[{"time":"2026-01-01T00:00:00Z","tags":["name=ok"],"id":"1","summary":{"data_added_packed":10}},
           {"time":"2026-01-01T00:00:00Z","tags":["name=broken"],"id":"2","summary":{"data_added_packed":20}}]'
    exit 0 ;;
  *name=ok*) echo '{"total_size":100}'; exit 0 ;;
  *name=broken*) echo "boom"; exit 1 ;;
esac
exit 2
`

func TestUpdateResticMetricsSkipsOnlyFailedStats(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "restic"), []byte(fakeRestic), 0755); err != nil {
		t.Fatalf("failed to write fake restic: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	r, err := restic.NewRestic(t.TempDir(), "password")
	if err != nil {
		t.Fatalf("failed to create restic: %v", err)
	}

	m := metrics.NewMetrics()
	m.SetResticTotalSizeByBackupName("broken", 999)

	c := config.Config{Backups: []config.BackupConfig{{Name: "ok"}, {Name: "broken"}}}
	err = updateResticMetrics(c, m, r)
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("expected stats error for broken, got %v", err)
	}

	rec := httptest.NewRecorder()
	m.GetMetricsHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)

	for _, want := range []string{
		`backup_restic_snapshot_total_size_bytes{backup_name="ok"} 100`,
		`backup_restic_snapshot_total_size_bytes{backup_name="broken"} 999`,
		`backup_restic_snapshot_count{backup_name="ok"} 1`,
		`backup_restic_snapshot_count{backup_name="broken"} 1`,
		`backup_restic_snapshot_latest_size_bytes{backup_name="broken"} 20`,
		`backup_scheduler_errors_total{operation="restic_get_snapshot_stats"} 1`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("expected metric %s", want)
		}
	}
	if t.Failed() {
		t.Logf("metrics:\n%s", body)
	}
}

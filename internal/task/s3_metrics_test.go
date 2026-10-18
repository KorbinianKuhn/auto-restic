package task

import (
	"reflect"
	"testing"
	"time"

	"github.com/korbiniankuhn/auto-restic/internal/s3"
)

func TestAggregateS3Stats(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(7 * 24 * time.Hour)

	objects := []s3.S3Object{
		// Two versions of a backup
		{Key: "data.tar.gz.age", BackupName: "data", Size: 100, CreatedAt: older},
		{Key: "data.tar.gz.age", BackupName: "data", Size: 200, CreatedAt: newer, IsLatest: true},
		// Deleted backup: the delete marker is the latest version
		{Key: "db.tar.gz.age", BackupName: "db", Size: 50, CreatedAt: older},
		{Key: "db.tar.gz.age", BackupName: "db", CreatedAt: newer, IsLatest: true, IsDeleteMarker: true},
		// Not a backup archive
		{Key: "notes.txt", BackupName: "notes.txt", Size: 10, CreatedAt: newer, IsLatest: true},
		{Key: "data", BackupName: "data", Size: 10, CreatedAt: newer, IsLatest: true},
		// Archive of a backup that is no longer configured
		{Key: "old.tar.gz.age", BackupName: "old", Size: 30, CreatedAt: newer, IsLatest: true},
	}

	got := aggregateS3Stats(objects, []string{"data", "db", "empty"})
	want := map[string]s3Stats{
		"data":  {count: 2, totalSize: 300, latestSize: 200, latestTime: float64(newer.Unix())},
		"db":    {count: 1, totalSize: 50},
		"empty": {},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected stats:\n got: %+v\nwant: %+v", got, want)
	}
}

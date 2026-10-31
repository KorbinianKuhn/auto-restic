package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// TestTriggerWaitsForRunningJob uses the scheduler setup of the server: a
// triggered job is queued behind a running job instead of running in parallel
func TestTriggerWaitsForRunningJob(t *testing.T) {
	scheduler, err := gocron.NewScheduler(gocron.WithLimitConcurrentJobs(1, gocron.LimitModeWait))
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Shutdown()

	release := make(chan struct{})
	started := make(chan string, 2)
	never := gocron.CronJob("0 0 0 1 1 *", true)

	slowJob, err := scheduler.NewJob(never, gocron.NewTask(func() {
		started <- "slow"
		<-release
	}))
	if err != nil {
		t.Fatal(err)
	}
	backupJob, err := scheduler.NewJob(never, gocron.NewTask(func() {
		started <- "backup"
	}))
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Start()

	server := httptest.NewServer(NewHandler("secret", map[string]Runner{"slow": slowJob, "backup": backupJob}))
	defer server.Close()

	if err := Trigger(server.URL, "secret", "slow"); err != nil {
		t.Fatalf("failed to trigger slow job: %v", err)
	}
	if got := receive(t, started); got != "slow" {
		t.Fatalf("expected slow job to start, got %s", got)
	}

	// Returns right away although the slow job blocks the scheduler
	if err := Trigger(server.URL, "secret", "backup"); err != nil {
		t.Fatalf("failed to trigger backup job: %v", err)
	}
	select {
	case got := <-started:
		t.Fatalf("expected %s to wait for the running job", got)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	if got := receive(t, started); got != "backup" {
		t.Fatalf("expected backup job to run after the slow job, got %s", got)
	}
}

func receive(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job")
		return ""
	}
}

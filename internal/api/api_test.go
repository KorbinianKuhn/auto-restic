package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRunner struct {
	runs int
	err  error
}

func (f *fakeRunner) RunNow() error {
	f.runs++
	return f.err
}

func TestTrigger(t *testing.T) {
	backup := &fakeRunner{}
	failing := &fakeRunner{err: errors.New("queue full")}
	server := httptest.NewServer(NewHandler("secret", map[string]Runner{
		"backup": backup,
		"prune":  failing,
	}))
	defer server.Close()

	if err := Trigger(server.URL, "secret", "backup"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backup.runs != 1 {
		t.Fatalf("expected backup to be queued once, got %d", backup.runs)
	}

	for _, tc := range []struct {
		name, token, job, want string
	}{
		{"wrong token", "wrong", "backup", "401"},
		{"empty token", "", "backup", "401"},
		{"unknown job", "secret", "unknown", "404"},
		{"run fails", "secret", "prune", "queue full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Trigger(server.URL, tc.token, tc.job)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
	if backup.runs != 1 {
		t.Fatalf("expected rejected requests not to queue a job, got %d runs", backup.runs)
	}
}

func TestOnlyPostIsAllowed(t *testing.T) {
	backup := &fakeRunner{}
	handler := NewHandler("secret", map[string]Runner{"backup": backup})

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/backup/run", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed || backup.runs != 0 {
		t.Fatalf("expected 405 without a run, got %d and %d runs", rec.Code, backup.runs)
	}
}

func TestEmptyServerTokenRejectsAll(t *testing.T) {
	handler := NewHandler("", map[string]Runner{"backup": &fakeRunner{}})

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/backup/run", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

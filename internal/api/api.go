package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Jobs that can be triggered via the API, in the order shown to users
var Jobs = []string{"backup", "s3", "check", "prune"}

// Runner queues a scheduled job for an immediate run. gocron.Job implements it.
type Runner interface {
	RunNow() error
}

// NewHandler serves POST /api/jobs/{job}/run. Requests must carry the token as
// bearer token. Triggered jobs are queued in the scheduler, so they run one at
// a time with the scheduled jobs and record the same metrics.
func NewHandler(token string, runners map[string]Runner) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/jobs/{job}/run", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		name := r.PathValue("job")
		runner, ok := runners[name]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown job %q, expected one of: %s", name, strings.Join(Jobs, ", ")), http.StatusNotFound)
			return
		}

		if err := runner.RunNow(); err != nil {
			slog.Error("failed to trigger job", "job", name, "error", err)
			http.Error(w, fmt.Sprintf("failed to trigger job %s: %s", name, err), http.StatusInternalServerError)
			return
		}

		slog.Info("job triggered via api", "job", name)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "job %s queued\n", name)
	})
	return mux
}

func authorized(r *http.Request, token string) bool {
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && token != "" && subtle.ConstantTimeCompare([]byte(given), []byte(token)) == 1
}

// requestTimeout bounds a trigger request. The server only queues the job, so
// it answers quickly even if another job is running.
const requestTimeout = 30 * time.Second

// Trigger asks the server at serverURL to queue job. It returns once the job is
// queued, not when it finished.
func Trigger(serverURL, token, job string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	endpoint, err := url.JoinPath(serverURL, "api", "jobs", job, "run")
	if err != nil {
		return fmt.Errorf("invalid server url %q: %w", serverURL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server responded with %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Jobs that can be triggered via the API, in the order shown to users
var Jobs = []string{"backup", "s3", "check", "prune"}

// SocketPath is where the server listens. A unix socket is only reachable
// from within the container (e.g. via docker exec), so it needs no
// authentication and no exposed port.
var SocketPath = filepath.Join(os.TempDir(), "auto-restic.sock")

// Runner queues a scheduled job for an immediate run. gocron.Job implements it.
type Runner interface {
	RunNow() error
}

// Server serves POST /api/jobs/{job}/run on a unix socket. Triggered jobs are
// queued in the scheduler, so they run one at a time with the scheduled jobs
// and record the same metrics.
type Server struct {
	http     *http.Server
	listener net.Listener
}

func NewServer(socketPath string, runners map[string]Runner) (*Server, error) {
	// A server that was killed leaves its socket behind
	if conn, err := net.Dial("unix", socketPath); err == nil {
		conn.Close()
		return nil, fmt.Errorf("another server is listening on %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}
	// Only the user running the server may trigger jobs
	if err := os.Chmod(socketPath, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to restrict socket permissions: %w", err)
	}

	return &Server{
		http: &http.Server{
			Handler:           newHandler(runners),
			ReadHeaderTimeout: 10 * time.Second,
		},
		listener: listener,
	}, nil
}

// Serve blocks until Shutdown is called
func (s *Server) Serve() error {
	if err := s.http.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops accepting jobs and removes the socket
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func newHandler(runners map[string]Runner) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/jobs/{job}/run", func(w http.ResponseWriter, r *http.Request) {
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

// requestTimeout bounds a trigger request. The server only queues the job, so
// it answers quickly even if another job is running.
const requestTimeout = 30 * time.Second

// Trigger asks the server listening on socketPath to queue job. It returns
// once the job is queued, not when it finished.
func Trigger(socketPath, job string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}}

	// The host is ignored, the transport always dials the socket
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://auto-restic/api/jobs/"+job+"/run", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach server on %s, is it running in this container?: %w", socketPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("server responded with %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

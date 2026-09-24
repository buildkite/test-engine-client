//go:build !windows

package runnerexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This is a real subprocess using the same socket/environment contract as Ruby.
func TestRunnerProcess(t *testing.T) {
	mode := os.Getenv("BKTEC_TEST_CHILD")
	if mode == "" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if mode == "startup" {
		time.Sleep(time.Minute)
		return
	}
	client := socketClient(os.Getenv(SocketEnv))
	defer client.CloseIdleConnections()
	session := handshake(t, client, "child")
	for {
		var response PullResponse
		data := request(t, client, "POST", "/v1/batches", session, `{}`, 200)
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		if response.Type == "done" {
			if mode == "flush-hang" {
				time.Sleep(time.Minute)
			}
			time.Sleep(100 * time.Millisecond)
			if err := os.WriteFile(os.Getenv("BKTEC_TEST_FLUSH"), []byte("flushed"), 0600); err != nil {
				t.Fatal(err)
			}
			return
		}
		if response.Type != "batch" {
			t.Fatalf("unexpected response: %s", data)
		}
		switch mode {
		case "crash":
			os.Exit(17)
		case "watchdog":
			time.Sleep(time.Minute)
		case "delete":
			request(t, client, "DELETE", "/v1/sessions/"+session, session, `{"reason":"leaving"}`, 200)
			return
		case "cancel", "interrupt", "terminate":
			sig := <-signals
			request(t, client, "DELETE", "/v1/sessions/"+session, session, fmt.Sprintf(`{"reason":%q}`, sig.String()), 200)
			return
		}
		request(t, client, "POST", "/v1/batches/"+response.Batch.ID+"/results", session, completed, 200)
	}
}

func TestLifecycle(t *testing.T) {
	for _, tc := range []struct{ mode, wantError string }{
		{"normal", ""}, {"startup", "startup timeout"}, {"watchdog", "batch timeout"}, {"crash", "runner exit"}, {"delete", "runner departed"}, {"cancel", "context canceled"}, {"interrupt", "interrupted"}, {"terminate", "interrupted"}, {"flush-hang", "shutdown timeout"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			flush := filepath.Join(t.TempDir(), "flushed")
			cmd := exec.Command(os.Args[0], "-test.run=^TestRunnerProcess$")
			cmd.Env = append(os.Environ(), "BKTEC_TEST_CHILD="+tc.mode, "BKTEC_TEST_FLUSH="+flush, SocketEnv+"=/wrong/socket")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			source := &testSource{batches: []Batch{{ID: "first"}, {ID: "retry"}}, done: "plan_completed"}
			called := make(chan struct{}, 1)
			source.onNext = func() {
				select {
				case called <- struct{}{}:
				default:
				}
			}
			if tc.mode == "cancel" || tc.mode == "interrupt" || tc.mode == "terminate" {
				go func() {
					select {
					case <-called:
					case <-ctx.Done():
						return
					}
					if tc.mode == "cancel" {
						cancel()
					} else {
						sig := syscall.SIGINT
						if tc.mode == "terminate" {
							sig = syscall.SIGTERM
						}
						_ = syscall.Kill(os.Getpid(), sig)
					}
				}()
			}
			opts := Options{StartupTimeout: 2 * time.Second, BatchTimeout: 2 * time.Second, ShutdownTimeout: 2 * time.Second}
			if tc.mode == "startup" {
				opts.StartupTimeout = 80 * time.Millisecond
				opts.ShutdownTimeout = 80 * time.Millisecond
			}
			if tc.mode == "watchdog" {
				opts.BatchTimeout = 80 * time.Millisecond
				opts.ShutdownTimeout = 80 * time.Millisecond
			}
			if tc.mode == "flush-hang" {
				opts.ShutdownTimeout = 80 * time.Millisecond
			}
			start := time.Now()
			err := Run(ctx, cmd, source, opts)
			t.Logf("%s duration: %s", tc.mode, time.Since(start))
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error %v, want %q", err, tc.wantError)
			}
			if tc.mode == "normal" {
				if data, err := os.ReadFile(flush); err != nil || string(data) != "flushed" {
					t.Fatalf("returned before flush: %q %v", data, err)
				}
				if fmt.Sprint(source.accepted) != "[first retry]" {
					t.Fatal(source.accepted)
				}
			}
			wantUnresolved := "[first]"
			if tc.mode == "normal" || tc.mode == "startup" || tc.mode == "flush-hang" {
				wantUnresolved = "[]"
			}
			if fmt.Sprint(source.unresolved) != wantUnresolved {
				t.Fatalf("unresolved %v want %s", source.unresolved, wantUnresolved)
			}
			for _, entry := range cmd.Env {
				if socket, ok := strings.CutPrefix(entry, SocketEnv+"="); ok {
					if _, err := os.Stat(filepath.Dir(socket)); !os.IsNotExist(err) {
						t.Fatalf("socket not cleaned up: %s %v", socket, err)
					}
				}
			}
			if cmd.ProcessState == nil {
				t.Fatal("child not reaped")
			}
		})
	}
}

func TestStartFailureCleansSocket(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	cmd := exec.Command("/nonexistent-bktec-runner")
	err := Run(context.Background(), cmd, &testSource{}, Options{})
	if err == nil || !strings.Contains(err.Error(), "start runner") {
		t.Fatalf("expected start failure, got %v", err)
	}
	files, err := os.ReadDir(os.Getenv("TMPDIR"))
	if err != nil || len(files) != 0 {
		t.Fatalf("socket left after failed start: %v, %v", files, err)
	}
}

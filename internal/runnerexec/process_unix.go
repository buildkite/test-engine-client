//go:build !windows

package runnerexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Run owns cmd.Start/Wait, its process group, the private socket and signal
// forwarding. The caller configures command arguments, directory, environment
// and output streams. A nil error means protocol completion and a clean child
// exit, not that tests passed. The source owns that decision.
func Run(ctx context.Context, cmd *exec.Cmd, source Source, opts Options) error {
	h, err := newHost(source, opts)
	if err != nil {
		return err
	}
	defer h.close()
	if err := ctx.Err(); err != nil {
		return err
	}
	env := cmd.Environ()
	cmd.Env = nil
	for _, value := range env {
		if !strings.HasPrefix(value, SocketEnv+"=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, SocketEnv+"="+h.socket)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = h.opts.ShutdownTimeout
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start runner: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	// Remove remaining descendants even when the group leader exits first.
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	startup := time.NewTimer(h.opts.StartupTimeout)
	defer startup.Stop()
	ready, done := h.ready, h.done
	var shutdown *time.Timer
	var shutdownC <-chan time.Time
	defer func() {
		if shutdown != nil {
			shutdown.Stop()
		}
	}()
	var cause error
	ctxDone := ctx.Done()
	fatal := h.fatal
	stop := func(reason error, sig syscall.Signal) {
		if cause != nil {
			return
		}
		cause = reason
		h.mu.Lock()
		h.stopping = true
		h.unresolved(reason)
		h.mu.Unlock()
		_ = syscall.Kill(-cmd.Process.Pid, sig)
		if shutdown != nil {
			shutdown.Stop()
		}
		shutdown = time.NewTimer(h.opts.ShutdownTimeout)
		shutdownC = shutdown.C
	}
	for {
		select {
		case <-ready:
			startup.Stop()
			ready = nil
		case <-startup.C:
			// Handshake may have completed concurrently with timer delivery.
			h.mu.Lock()
			started := h.instance != ""
			h.mu.Unlock()
			if !started {
				stop(errors.New("runner startup timeout"), syscall.SIGTERM)
			}
		case <-done:
			done = nil
			if shutdown == nil {
				shutdown = time.NewTimer(h.opts.ShutdownTimeout)
				shutdownC = shutdown.C
			}
		case err := <-fatal:
			fatal = nil
			stop(err, syscall.SIGTERM)
		case <-ctxDone:
			ctxDone = nil
			stop(ctx.Err(), syscall.SIGTERM)
		case sig := <-signals:
			stop(fmt.Errorf("runner interrupted by %s", sig), sig.(syscall.Signal))
		case <-shutdownC:
			if cause == nil {
				cause = errors.New("runner shutdown timeout waiting for collector flush")
			}
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-exited
			return cause
		case err := <-exited:
			h.mu.Lock()
			complete := h.terminal != ""
			if cause == nil {
				select {
				case cause = <-h.fatal:
				default:
				}
			}
			if cause == nil && err != nil {
				cause = fmt.Errorf("runner exit: %w", err)
			}
			if cause == nil && !complete {
				cause = errors.New("runner exited before done")
			}
			if cause != nil {
				h.stopping = true
				h.unresolved(cause)
			}
			h.mu.Unlock()
			return cause
		}
	}
}

package bes

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/buildkite/test-engine-client/internal/env"
	"github.com/buildkite/test-engine-client/internal/upload"
	"google.golang.org/genproto/googleapis/devtools/build/v1"
	"google.golang.org/grpc"
)

func ListenCLI(argv []string, env env.Env) error {
	flags := flag.NewFlagSet("bktec bazel listen", flag.ExitOnError)
	portFlag := flags.Int("port", 0, "gRPC port to listen")
	listenHostFlag := flags.String("listen-host", "127.0.0.1", "gRPC host to listen")
	debugFlag := flags.Bool("debug", false, "debug logging")
	flags.Parse(argv)

	if *debugFlag {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// a channel to propagate OS signals
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)

	// configure uploader
	cfg, err := upload.ConfigFromEnv(env)
	if err != nil {
		return fmt.Errorf("uploader configuration: %w", err)
	}
	runEnv, err := upload.RunEnvFromEnv(env)
	if err != nil {
		return fmt.Errorf("uploader run_env configuration: %w", err)
	}
	uploader := NewUploader(cfg, runEnv, "junit")
	go uploader.Start(ctx)

	// configure gRPC Bazel BES server
	addr := fmt.Sprintf("%s:%d", *listenHostFlag, *portFlag)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	opts := []grpc.ServerOption{}
	srv := grpc.NewServer(opts...)
	build.RegisterPublishBuildEventServer(srv, BuildEventServer{
		handler: &BuildEventHandler{
			filenames: uploader.Filenames,
		},
	})
	slog.Info("Bazel BES listener", "addr", "grpc://"+listener.Addr().String())
	go serve(srv, listener)

	// main loop
	run := true
	sigCount := 0
	for run {
		select {
		case url, ok := <-uploader.Responses:
			if !ok {
				slog.Debug("Response channel closed")
				run = false
				continue
			}
			slog.Info("Uploaded", "url", url)
		case err := <-uploader.Errs:
			slog.Error("Upload error", "error", err)
		case sig := <-signals:
			sigCount++
			srv.Stop()
			if sigCount == 1 {
				slog.Info("Stopping (again to force)...", "signal", sig)
				uploader.Stop()
			} else {
				slog.Info("Stopping forcefully...", "signal", sig)
				cancel()
			}
		}
	}

	slog.Debug("done")
	return nil
}

func serve(s *grpc.Server, listener net.Listener) {
	err := s.Serve(listener)
	if err != nil {
		slog.Error("gRPC server error", "err", err)
	}
}

// Package quietslog provides a replacement for slog which downgrades Info
// messages to Debug instead, so that the log output is quieter. This is done
// specifically so that bes.BuildEventServer.PublishBuildToolEventStream()
// source code can be kept unmodified from the bb-portal upstream it's copied
// from.
package quietslog

import (
	"context"
	"log/slog"
)

// InfoContext delegates to DebugContext of the real logger, making this logger quiet.
func InfoContext(ctx context.Context, msg string, args ...any) {
	slog.DebugContext(ctx, msg, args...)
}

// WarnContext wraps the direct logger directly.
func WarnContext(ctx context.Context, msg string, args ...any) {
	slog.WarnContext(ctx, msg, args...)
}

// ErrorContext wraps the direct logger directly.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	slog.ErrorContext(ctx, msg, args...)
}

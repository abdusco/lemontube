// Package log carries structured loggers through contexts.
package log

import (
	"context"
	"log/slog"
)

type contextKey struct{}

// ToCtx returns a context carrying logger.
func ToCtx(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromCtx returns the context's logger, or the default logger if none is set.
func FromCtx(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

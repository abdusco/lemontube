package log

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := ToCtx(t.Context(), logger)
	child, cancel := context.WithCancel(ctx)
	cancel()

	assert.Same(t, logger, FromCtx(child))
	assert.Same(t, logger, FromCtx(context.WithoutCancel(child)))
	assert.Same(t, slog.Default(), FromCtx(t.Context()))
}

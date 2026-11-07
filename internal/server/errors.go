package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"lemontube/internal"
	"lemontube/internal/log"

	"github.com/labstack/echo/v5"
)

// operationError keeps the cause for logging while exposing only expected input errors.
func operationError(operation string, err error) error {
	wrapped := fmt.Errorf("%s: %w", operation, err)
	var input *internal.InputError
	if errors.As(err, &input) {
		return echo.NewHTTPError(http.StatusBadRequest, input.Error()).Wrap(wrapped)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return echo.NewHTTPError(http.StatusNotFound, "Not Found").Wrap(wrapped)
	}
	return wrapped
}

func handleHTTPError(c *echo.Context, err error) {
	status := echo.StatusCode(err)
	if status == 0 {
		status = http.StatusInternalServerError
	}
	log.FromCtx(c.Request().Context()).Error("HTTP request failed",
		"method", c.Request().Method,
		"path", c.Request().URL.Path,
		"status", status,
		"error", err,
	)
	echo.DefaultHTTPErrorHandler(false)(c, err)
}

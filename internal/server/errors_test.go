package server

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"

	"lemontube/internal"
	"lemontube/internal/log"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPErrorHandler(t *testing.T) {
	secret := errors.New("database password is secret")
	tests := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"internal failure", operationError("list jobs", secret), http.StatusInternalServerError, "Internal Server Error"},
		{"invalid input", operationError("enqueue", &internal.InputError{Err: errors.New("invalid URL")}), http.StatusBadRequest, "invalid URL"},
		{"missing resource", operationError("find download", fs.ErrNotExist), http.StatusNotFound, "Not Found"},
		{"echo sentinel", echo.ErrForbidden, http.StatusForbidden, "Forbidden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			e := echo.NewWithConfig(echo.Config{HTTPErrorHandler: handleHTTPError})
			e.GET("/test", func(c *echo.Context) error { return tt.err })
			request := httptest.NewRequest(http.MethodGet, "/test?token=secret", nil)
			request = request.WithContext(log.ToCtx(request.Context(), logger))
			response := httptest.NewRecorder()
			e.ServeHTTP(response, request)

			assert.Equal(t, tt.status, response.Code)
			assert.JSONEq(t, `{"message":`+strconv.Quote(tt.message)+`}`, response.Body.String())
			assert.Contains(t, logs.String(), tt.err.Error())
			assert.NotContains(t, logs.String(), "token=secret")
		})
	}
	wrapped := operationError("list jobs", secret)
	require.ErrorIs(t, wrapped, secret)
}

func TestDatabaseFailureIsInternalError(t *testing.T) {
	dir := t.TempDir()
	repo, err := internal.OpenRepository(filepath.Join(dir, "state.sqlite3"))
	require.NoError(t, err)
	service, err := internal.NewService(repo, internal.Config{
		DownloadDir: filepath.Join(dir, "downloads"),
		TempDir:     filepath.Join(dir, "temp"),
	})
	require.NoError(t, err)
	require.NoError(t, repo.Close())

	var logs bytes.Buffer
	request := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	request = request.WithContext(log.ToCtx(request.Context(), slog.New(slog.NewTextHandler(&logs, nil))))
	response := httptest.NewRecorder()
	New(fstest.MapFS{}, service, nil).ServeHTTP(response, request)

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.JSONEq(t, `{"message":"Internal Server Error"}`, response.Body.String())
	assert.Contains(t, logs.String(), "list jobs")
	assert.Contains(t, logs.String(), "closed")
}

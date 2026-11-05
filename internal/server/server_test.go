package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lemontube/internal"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	repo, err := internal.OpenRepository(filepath.Join(dir, "state.sqlite3"))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, repo.Close())
	})
	service, err := internal.NewService(repo, internal.Config{
		DownloadDir: filepath.Join(dir, "downloads"),
		TempDir:     filepath.Join(dir, "temp"),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("LemonTube")},
	}
	return New(assets, service)
}

func TestHTTPAPI(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		assertResult func(t *testing.T, response *httptest.ResponseRecorder)
	}{
		{
			name:   "static assets must revalidate",
			method: http.MethodGet,
			path:   "/",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
			},
		},
		{
			name:   "create download",
			method: http.MethodPost,
			path:   "/api/jobs",
			body:   `{"url":"https://example.com/video","download_type":"video","options":{"format":"mp4"}}`,
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusCreated, response.Code)
				assert.Contains(t, response.Body.String(), `"id"`)
			},
		},
		{
			name:   "reject local URL",
			method: http.MethodPost,
			path:   "/api/jobs",
			body:   `{"url":"file:///etc/passwd","download_type":"video"}`,
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusBadRequest, response.Code)
			},
		},
		{
			name:   "list downloads",
			method: http.MethodGet,
			path:   "/api/jobs",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusOK, response.Code)
				assert.JSONEq(t, `[]`, response.Body.String())
			},
		},
		{
			name:   "untracked download",
			method: http.MethodGet,
			path:   "/download/secret.txt",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusNotFound, response.Code)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t)
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			tt.assertResult(t, response)
		})
	}
}

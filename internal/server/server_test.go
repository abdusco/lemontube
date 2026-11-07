package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lemontube/internal"
)

func newTestServer(t *testing.T) http.Handler {
	return newTestServerWithUsers(t, nil)
}

func newTestServerWithUsers(t *testing.T, users []AuthUser) http.Handler {
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
		"index.html":    &fstest.MapFile{Data: []byte("LemonTube")},
		"login.html":    &fstest.MapFile{Data: []byte("Sign in")},
		"styles.css":    &fstest.MapFile{Data: []byte("body {}")},
		"lemontube.svg": &fstest.MapFile{Data: []byte("<svg></svg>")},
		"app.js":        &fstest.MapFile{Data: []byte("app")},
	}
	return New(assets, service, users)
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

func TestDownloadEncodedFilename(t *testing.T) {
	dir := t.TempDir()
	repo, err := internal.OpenRepository(filepath.Join(dir, "state.sqlite3"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, repo.Close()) })
	downloadDir := filepath.Join(dir, "downloads")
	service, err := internal.NewService(repo, internal.Config{
		DownloadDir: downloadDir,
		TempDir:     filepath.Join(dir, "temp"),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	const filename = "What happens if you open the window at  -60°C⧸-76°F  (Yakutia, Russia) [ZvIGTm3PAZQ].mp4"
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, filename), []byte("video"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, "100%2F.mp4"), []byte("percent"), 0o600))
	require.NoError(t, repo.CreateJob(t.Context(), internal.Job{
		ID:           "0123456789abcdef0123456789abcdef",
		URL:          "https://example.com/video",
		Title:        "video",
		DownloadType: internal.DownloadVideo,
	}))
	require.NoError(t, repo.SetOutputs(t.Context(), "0123456789abcdef0123456789abcdef", []internal.OutputFile{
		{
			Path:  filename,
			State: internal.OutputPublished,
		},
		{
			Path:  "100%2F.mp4",
			State: internal.OutputPublished,
		},
	}))
	server := New(fstest.MapFS{}, service, nil)
	for _, tt := range []struct {
		name         string
		path         string
		assertResult func(t *testing.T, response *httptest.ResponseRecorder)
	}{
		{
			name: "encoded punctuation and unicode",
			path: "/download/What%20happens%20if%20you%20open%20the%20window%20at%20%20-60%C2%B0C%E2%A7%B8-76%C2%B0F%20%20(Yakutia%2C%20Russia)%20%5BZvIGTm3PAZQ%5D.mp4",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "video", response.Body.String())
			},
		},
		{
			name: "literal percent escape",
			path: "/download/100%252F.mp4",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "percent", response.Body.String())
			},
		},
		{
			name: "literal percent escape with raw path",
			path: "/download/100%252F%2Emp4",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				require.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "percent", response.Body.String())
			},
		},
		{
			name: "encoded path separator",
			path: "/download/..%2Fsecret.mp4",
			assertResult: func(t *testing.T, response *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusNotFound, response.Code)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			tt.assertResult(t, response)
		})
	}
}

func TestDeleteCookiesForOneDomain(t *testing.T) {
	server := newTestServer(t)
	content := `.example.com	TRUE	/	TRUE	0	session	secret
.other.example	TRUE	/	FALSE	0	key	value
`

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/cookies", strings.NewReader(content)))
	require.Equal(t, http.StatusNoContent, response.Code)

	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/cookies/.example.com", nil))
	require.Equal(t, http.StatusNoContent, response.Code)

	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/cookies", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `[".other.example"]`, response.Body.String())
}

package internal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()
	r, err := OpenRepository(filepath.Join(t.TempDir(), "test.sqlite3"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, r.Close()) })
	return r
}

func TestOpenRepositoryWithRelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	repo, err := OpenRepository("lemontube.sqlite3")
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, repo.Close())
	})
	assert.FileExists(t, filepath.Join(dir, "lemontube.sqlite3"))
}

func TestFinishedTimestampIsReadable(t *testing.T) {
	r := newTestRepository(t)
	job := Job{
		ID:           "finished",
		URL:          "https://example.com",
		Title:        "Example",
		DownloadType: DownloadVideo,
	}
	require.NoError(t, r.CreateJob(t.Context(), job))
	require.NoError(t, r.FinishJob(t.Context(), job.ID, JobFinished, nil))
	got, err := r.GetJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.NotNil(t, got.FinishedAt)
	_, err = time.Parse(time.RFC3339Nano, *got.FinishedAt)
	require.NoError(t, err)
}

func TestRepositoryJobRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		job  Job
	}{
		{"video", Job{ID: "video", URL: "https://example.com/v", Title: "Video", DownloadType: "video", Options: DownloadOptions{Format: "mp4", SubtitleLangs: []string{"en"}}}},
		{"audio", Job{ID: "audio", URL: "https://example.com/a", Title: "Audio", DownloadType: "audio", Options: DownloadOptions{Format: "mp3"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepository(t)
			require.NoError(t, r.CreateJob(t.Context(), tt.job))
			got, err := r.GetJob(t.Context(), tt.job.ID)
			require.NoError(t, err)
			assert.Equal(t, JobQueued, got.Status)
			assert.Equal(t, tt.job.Options, got.Options)
			assert.NotEmpty(t, got.CreatedAt)
		})
	}
}

func TestRepositoryConstraints(t *testing.T) {
	tests := []struct{ name, query string }{
		{"invalid status", "INSERT INTO jobs (id, url, title, download_type, status) VALUES ('1', 'url', 'title', 'video', 'bogus')"},
		{"invalid download type", "INSERT INTO jobs (id, url, title, download_type) VALUES ('1', 'url', 'title', 'bogus')"},
		{"missing foreign key", "INSERT INTO job_logs (job_id, message) VALUES ('missing', 'hello')"},
		{"invalid json", "INSERT INTO jobs (id, url, title, download_type, options) VALUES ('1', 'url', 'title', 'video', 'invalid')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestRepository(t)
			_, err := r.sql.ExecContext(t.Context(), tt.query)
			require.Error(t, err)
		})
	}
}

func TestJSONScan(t *testing.T) {
	tests := []struct {
		name         string
		input        any
		assertResult func(t *testing.T, value JSON[[]string], err error)
	}{
		{
			name:  "bytes",
			input: []byte(`["en"]`),
			assertResult: func(t *testing.T, value JSON[[]string], err error) {
				require.NoError(t, err)
				assert.Equal(t, []string{"en"}, value.Data)
			},
		},
		{
			name:  "string",
			input: `["tr"]`,
			assertResult: func(t *testing.T, value JSON[[]string], err error) {
				require.NoError(t, err)
				assert.Equal(t, []string{"tr"}, value.Data)
			},
		},
		{
			name: "null",
			assertResult: func(t *testing.T, value JSON[[]string], err error) {
				require.NoError(t, err)
				assert.Nil(t, value.Data)
			},
		},
		{
			name:  "invalid",
			input: "invalid",
			assertResult: func(t *testing.T, _ JSON[[]string], err error) {
				require.Error(t, err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value JSON[[]string]
			err := value.Scan(tt.input)
			tt.assertResult(t, value, err)
		})
	}
}

package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T) (*Service, *Repository) {
	t.Helper()
	repo := newTestRepository(t)
	dir := t.TempDir()
	service, err := NewService(repo, Config{DownloadDir: filepath.Join(dir, "downloads"), TempDir: filepath.Join(dir, "temp")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	return service, repo
}

func TestPublishAvoidsCollisionsAndTracksFiles(t *testing.T) {
	s, repo := newTestService(t)
	id := "0123456789abcdef0123456789abcdef"
	require.NoError(t, repo.CreateJob(t.Context(), Job{ID: id, URL: "https://example.com", Title: "example", DownloadType: DownloadVideo}))
	workspace := filepath.Join(s.config.TempDir, ".lemontube", id)
	require.NoError(t, os.Mkdir(workspace, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "video.mp4"), []byte("download"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(s.config.DownloadDir, "video.mp4"), []byte("existing"), 0o600))
	require.NoError(t, s.publish(t.Context(), id, workspace))
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, job.Outputs, 1)
	assert.Equal(t, "video (1).mp4", job.Outputs[0].Path)
	assert.Equal(t, OutputPublished, job.Outputs[0].State)
	data, err := os.ReadFile(filepath.Join(s.config.DownloadDir, "video.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "existing", string(data))
}

func TestDeleteOutputsProtectsChangedFiles(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		assertResult func(t *testing.T, path string, err error)
	}{
		{
			name:    "unchanged",
			content: "original",
			assertResult: func(t *testing.T, path string, err error) {
				require.NoError(t, err)
				assert.NoFileExists(t, path)
			},
		},
		{
			name:    "changed",
			content: "replacement",
			assertResult: func(t *testing.T, path string, err error) {
				require.Error(t, err)
				assert.FileExists(t, path)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestService(t)
			path := filepath.Join(s.config.DownloadDir, "video.mp4")
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))
			digest := sha256.Sum256([]byte("original"))
			err := s.deleteOutputs([]OutputFile{{Path: "video.mp4", State: "published", SHA256: hex.EncodeToString(digest[:])}})
			tt.assertResult(t, path, err)
		})
	}
}

func TestClearOrphanOutputs(t *testing.T) {
	s, repo := newTestService(t)
	ctx := t.Context()
	digest := sha256.Sum256([]byte("original"))
	for _, item := range []struct{ id, name, content string }{
		{"0123456789abcdef0123456789abcdef", "owned.mp4", "original"},
		{"1123456789abcdef0123456789abcdef", "changed.mp4", "changed"},
	} {
		require.NoError(t, repo.CreateJob(ctx, Job{ID: item.id, URL: "https://example.com", Title: item.name, DownloadType: DownloadVideo}))
		require.NoError(t, os.WriteFile(filepath.Join(s.config.DownloadDir, item.name), []byte(item.content), 0o600))
		require.NoError(t, repo.SetOutputs(ctx, item.id, []OutputFile{{Path: item.name, Stage: ".lemontube-" + item.id + "-0", State: "published", SHA256: hex.EncodeToString(digest[:])}}))
		_, err := repo.RequestDelete(ctx, item.id, false)
		require.NoError(t, err)
	}
	unowned := filepath.Join(s.config.DownloadDir, "unowned.mp4")
	require.NoError(t, os.WriteFile(unowned, []byte("original"), 0o600))
	cleared, retained, err := s.ClearOrphanOutputs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, cleared)
	assert.Equal(t, 1, retained)
	assert.NoFileExists(t, filepath.Join(s.config.DownloadDir, "owned.mp4"))
	assert.FileExists(t, filepath.Join(s.config.DownloadDir, "changed.mp4"))
	assert.FileExists(t, unowned)
	jobs, err := repo.ListJobs(ctx, false)
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

func TestReconcilePublishedHardLink(t *testing.T) {
	s, repo := newTestService(t)
	id := "0123456789abcdef0123456789abcdef"
	stage := ".lemontube-" + id + "-0"
	require.NoError(t, repo.CreateJob(t.Context(), Job{ID: id, URL: "https://example.com", Title: "example", DownloadType: DownloadVideo}))
	require.NoError(t, os.WriteFile(filepath.Join(s.config.DownloadDir, stage), []byte("download"), 0o600))
	require.NoError(t, os.Link(filepath.Join(s.config.DownloadDir, stage), filepath.Join(s.config.DownloadDir, "video.mp4")))
	outputs := []OutputFile{{Path: "video.mp4", Stage: stage, State: "publishing"}}
	require.NoError(t, repo.SetOutputs(t.Context(), id, outputs))
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	require.NoError(t, s.recoverInterruptedOutputs(t.Context(), &job))
	got, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, OutputPublished, got.Outputs[0].State)
	assert.NoFileExists(t, filepath.Join(s.config.DownloadDir, stage))
	assert.FileExists(t, filepath.Join(s.config.DownloadDir, "video.mp4"))
}

func TestSweepCleansInterruptedWorkspaceAndPreservesUnownedFiles(t *testing.T) {
	s, repo := newTestService(t)
	id := "0123456789abcdef0123456789abcdef"
	require.NoError(t, repo.CreateJob(t.Context(), Job{
		ID:           id,
		URL:          "https://example.com",
		Title:        "example",
		DownloadType: DownloadVideo,
	}))
	claimed, err := repo.ClaimJob(t.Context(), id)
	require.NoError(t, err)
	require.True(t, claimed)
	workspace := filepath.Join(s.config.TempDir, ".lemontube", id)
	require.NoError(t, os.Mkdir(workspace, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "partial.part"), []byte("partial"), 0o600))
	unowned := filepath.Join(s.config.DownloadDir, "unowned.part")
	require.NoError(t, os.WriteFile(unowned, []byte("keep"), 0o600))
	require.NoError(t, repo.RecoverJobs(t.Context()))
	require.NoError(t, s.sweep(t.Context()))
	assert.NoDirExists(t, workspace)
	assert.FileExists(t, unowned)
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, JobFailed, job.Status)
}

func TestSweepRemovesOnlyOldOrphanWorkspaces(t *testing.T) {
	s, _ := newTestService(t)
	root := filepath.Join(s.config.TempDir, ".lemontube")
	old := filepath.Join(root, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	recent := filepath.Join(root, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	unrecognized := filepath.Join(root, "not-a-job")
	for _, path := range []string{old, recent, unrecognized} {
		require.NoError(t, os.Mkdir(path, 0o700))
	}
	aged := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(old, aged, aged))
	require.NoError(t, os.Chtimes(unrecognized, aged, aged))
	require.NoError(t, s.sweep(t.Context()))
	assert.NoDirExists(t, old)
	assert.DirExists(t, recent)
	assert.DirExists(t, unrecognized)
}

func TestSweepRetainsFailedDeletionForRetry(t *testing.T) {
	s, repo := newTestService(t)
	id := "0123456789abcdef0123456789abcdef"
	require.NoError(t, repo.CreateJob(t.Context(), Job{
		ID:           id,
		URL:          "https://example.com",
		Title:        "example",
		DownloadType: DownloadVideo,
	}))
	require.NoError(t, repo.FinishJob(t.Context(), id, JobFinished, nil))
	path := filepath.Join(s.config.DownloadDir, "video.mp4")
	require.NoError(t, os.WriteFile(path, []byte("changed"), 0o600))
	digest := sha256.Sum256([]byte("original"))
	require.NoError(t, repo.SetOutputs(t.Context(), id, []OutputFile{{
		Path:   "video.mp4",
		Stage:  ".lemontube-" + id + "-0",
		State:  "published",
		SHA256: hex.EncodeToString(digest[:]),
	}}))
	found, err := repo.RequestDelete(t.Context(), id, true)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, s.sweep(t.Context()))
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, job.CleanupError)
	assert.Contains(t, *job.CleanupError, "changed")
	assert.FileExists(t, path)
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	require.NoError(t, repo.ClearCleanupError(t.Context(), id))
	require.NoError(t, s.sweep(t.Context()))
	_, err = repo.GetJob(t.Context(), id)
	assert.Error(t, err)
	assert.NoFileExists(t, path)
}

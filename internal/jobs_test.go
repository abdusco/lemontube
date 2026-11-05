package internal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"lemontube/internal/ytdlp"
)

func TestServiceRunsAndCleansJob(t *testing.T) {
	s, repo := newTestService(t)
	binary := filepath.Join(t.TempDir(), "fake-ytdlp")
	script := "#!/bin/sh\nwhile [ \"$1\" != \"--paths\" ]; do shift; done\nshift\nprintf 'media' > \"$1/result.mp4\"\nprintf 'download complete\\n'\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o700))
	s.config.YTDLPPath = binary
	s.ytdlp = ytdlp.NewManager(filepath.Join(t.TempDir(), "managed"), binary)
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, s.Start(ctx))
	t.Cleanup(func() { cancel(); s.Wait() })
	id, err := s.Enqueue(t.Context(), "https://example.com/video", DownloadVideo, DownloadOptions{Format: "mp4", Quality: "best"})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		job, err := repo.GetJob(t.Context(), id)
		return err == nil && job.Status == JobFinished
	}, 5*time.Second, 20*time.Millisecond)
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, job.Outputs, 1)
	assert.FileExists(t, filepath.Join(s.config.DownloadDir, "result.mp4"))
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(s.config.TempDir, ".lemontube", id))
		return os.IsNotExist(err)
	}, time.Second, 10*time.Millisecond)
	logs, err := repo.Logs(t.Context(), id)
	require.NoError(t, err)
	assert.Contains(t, logs, "download complete")
}

func TestEnqueueValidation(t *testing.T) {
	tests := []struct {
		name, url string
		kind      DownloadType
		options   DownloadOptions
	}{
		{"invalid URL", "file:///tmp/video", DownloadVideo, DownloadOptions{}},
		{"invalid type", "https://example.com", "other", DownloadOptions{}},
		{"invalid quality", "https://example.com", DownloadVideo, DownloadOptions{Quality: "1]bad"}},
		{"invalid format", "https://example.com", DownloadAudio, DownloadOptions{Format: "bad"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestService(t)
			_, err := s.Enqueue(t.Context(), tt.url, tt.kind, tt.options)
			require.Error(t, err)
		})
	}
}

func TestDeleteRunningJobCleansWorkspace(t *testing.T) {
	s, repo := newTestService(t)
	binary := filepath.Join(t.TempDir(), "fake-ytdlp")
	script := "#!/bin/sh\nwhile [ \"$1\" != \"--paths\" ]; do shift; done\nshift\nprintf 'partial' > \"$1/incomplete.part\"\nsleep 60\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o700))
	s.config.YTDLPPath = binary
	s.ytdlp = ytdlp.NewManager(filepath.Join(t.TempDir(), "managed"), binary)
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, s.Start(ctx))
	t.Cleanup(func() {
		cancel()
		s.Wait()
	})
	id, err := s.Enqueue(t.Context(), "https://example.com/video", DownloadVideo, DownloadOptions{})
	require.NoError(t, err)
	workspace := filepath.Join(s.config.TempDir, ".lemontube", id)
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(workspace, "incomplete.part"))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, s.Delete(t.Context(), id))
	require.Eventually(t, func() bool {
		_, err := repo.GetJob(t.Context(), id)
		return err != nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.NoDirExists(t, workspace)
}

func TestCookiesAreSavedByDomain(t *testing.T) {
	s, _ := newTestService(t)
	content := "# Netscape HTTP Cookie File\n" +
		"#HttpOnly_.example.com\tTRUE\t/\tTRUE\t0\tsession\tsecret\n" +
		".other.example\tTRUE\t/\tFALSE\t0\tkey\tvalue\n"
	require.NoError(t, s.SaveCookies(t.Context(), content))
	domains, err := s.Cookies(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{".example.com", ".other.example"}, domains)
	_, stored, err := s.repo.Cookies(t.Context())
	require.NoError(t, err)
	assert.Contains(t, stored, "#HttpOnly_.example.com")
	require.NoError(t, s.DeleteCookies(t.Context(), ".example.com"))
	domains, err = s.Cookies(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{".other.example"}, domains)
}

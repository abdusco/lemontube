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

func TestExecuteJobWorkspaceError(t *testing.T) {
	s := &Service{}
	err := s.executeJob(t.Context(), Job{}, t.TempDir())
	require.ErrorContains(t, err, "create workspace")
	require.ErrorIs(t, err, os.ErrExist)
}

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
	id, err := s.Enqueue(t.Context(), EnqueueParams{
		URL:          "https://example.com/video",
		DownloadType: DownloadVideo,
		Options:      DownloadOptions{Format: "mp4", Quality: "best"},
	})
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
		_, err := os.Stat(filepath.Join(s.config.TempDir, ".lemontube", string(id)))
		return os.IsNotExist(err)
	}, time.Second, 10*time.Millisecond)
	logs, err := repo.Logs(t.Context(), id)
	require.NoError(t, err)
	assert.Contains(t, logs, "download complete")
}

func TestCookieFailureIdentifiesSubmittedSite(t *testing.T) {
	s, repo := newTestService(t)
	binary := filepath.Join(t.TempDir(), "fake-ytdlp")
	script := "#!/bin/sh\nprintf '%s\\n' 'ERROR: Sign in. Use --cookies for authentication.' >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o700))
	s.ytdlp = ytdlp.NewManager(filepath.Join(t.TempDir(), "managed"), binary)
	ctx, cancel := context.WithCancel(t.Context())
	require.NoError(t, s.Start(ctx))
	t.Cleanup(func() { cancel(); s.Wait() })
	id, err := s.Enqueue(t.Context(), EnqueueParams{
		URL:          "https://video.example.com:8443/watch?token=secret",
		DownloadType: DownloadVideo,
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		job, err := repo.GetJob(t.Context(), id)
		return err == nil && job.Status == JobFailed
	}, 5*time.Second, 20*time.Millisecond)
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, job.Error)
	assert.Equal(t, "Site authentication failed. Add or update cookies for video.example.com in Site cookies, then retry.", *job.Error)
}

func TestEnqueueValidation(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		kind    DownloadType
		options DownloadOptions
	}{
		{
			name: "invalid URL",
			url:  "file:///tmp/video",
			kind: DownloadVideo,
		},
		{
			name: "invalid type",
			url:  "https://example.com",
			kind: "other",
		},
		{
			name:    "invalid quality",
			url:     "https://example.com",
			kind:    DownloadVideo,
			options: DownloadOptions{Quality: "1]bad"},
		},
		{
			name:    "invalid format",
			url:     "https://example.com",
			kind:    DownloadAudio,
			options: DownloadOptions{Format: "bad"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestService(t)
			params := EnqueueParams{URL: tt.url, DownloadType: tt.kind, Options: tt.options}
			require.Error(t, params.Validate())
			_, err := s.Enqueue(t.Context(), params)
			require.Error(t, err)
		})
	}
}

func TestEnqueueDefaults(t *testing.T) {
	s, repo := newTestService(t)
	params := EnqueueParams{
		URL:          "https://example.com/video",
		DownloadType: DownloadVideo,
	}
	require.NoError(t, params.Validate())
	id, err := s.Enqueue(t.Context(), params)
	require.NoError(t, err)
	job, err := repo.GetJob(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, DownloadOptions{Format: "mp4", Quality: "best"}, job.Options)
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
	id, err := s.Enqueue(t.Context(), EnqueueParams{
		URL:          "https://example.com/video",
		DownloadType: DownloadVideo,
	})
	require.NoError(t, err)
	workspace := filepath.Join(s.config.TempDir, ".lemontube", string(id))
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

func TestParseCookiesTXT(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		assertResult func(t *testing.T, cookies Cookies, err error)
	}{
		{
			name: "groups cookies and preserves http-only rows",
			text: `# Netscape HTTP Cookie File
.example.com	TRUE	/	TRUE	0	a	one
#HttpOnly_.example.com	TRUE	/	TRUE	0	b	two
other.example	FALSE	/	FALSE	0	c	three
`,
			assertResult: func(t *testing.T, cookies Cookies, err error) {
				require.NoError(t, err)
				assert.Equal(t, Cookies{
					".example.com": `.example.com	TRUE	/	TRUE	0	a	one
#HttpOnly_.example.com	TRUE	/	TRUE	0	b	two`,
					"other.example": "other.example\tFALSE\t/\tFALSE\t0\tc\tthree",
				}, cookies)
			},
		},
		{
			name: "skips malformed rows when another row is valid",
			text: `bad row
	TRUE	/	TRUE	0	a	b
example.com	FALSE	/	FALSE	0	a	b`,
			assertResult: func(t *testing.T, cookies Cookies, err error) {
				require.NoError(t, err)
				assert.Equal(t, Cookies{"example.com": "example.com\tFALSE\t/\tFALSE\t0\ta\tb"}, cookies)
			},
		},
		{
			name: "rejects input without valid rows",
			text: `# comment
invalid`,
			assertResult: func(t *testing.T, cookies Cookies, err error) {
				require.ErrorContains(t, err, "no valid cookies found")
				assert.Nil(t, cookies)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cookies, err := ParseCookiesTXT(tt.text)
			tt.assertResult(t, cookies, err)
		})
	}
}

func TestCookiesAreSavedByDomain(t *testing.T) {
	s, _ := newTestService(t)
	content := `# Netscape HTTP Cookie File
#HttpOnly_.example.com	TRUE	/	TRUE	0	session	secret
.other.example	TRUE	/	FALSE	0	key	value
`
	require.NoError(t, s.SaveCookies(t.Context(), content))
	domains, err := s.Cookies(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{".example.com", ".other.example"}, domains)
	stored, err := s.repo.Cookies(t.Context())
	require.NoError(t, err)
	assert.Contains(t, stored.NetscapeTXT(), "#HttpOnly_.example.com")
	require.NoError(t, s.DeleteCookies(t.Context(), ".example.com"))
	domains, err = s.Cookies(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{".other.example"}, domains)
}

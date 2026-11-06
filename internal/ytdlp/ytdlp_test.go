package ytdlp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinarySource(t *testing.T) {
	for _, tt := range []struct {
		name     string
		value    BinarySource
		expected bool
	}{
		{
			name:     "external",
			value:    SourceExternal,
			expected: true,
		},
		{
			name:     "managed",
			value:    SourceManaged,
			expected: true,
		},
		{
			name:     "system",
			value:    SourceSystem,
			expected: true,
		},
		{
			name:  "invalid",
			value: "unknown",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.value.Validate() == nil)
		})
	}
}

func TestBinarySourceJSON(t *testing.T) {
	data, err := json.Marshal(Info{Source: SourceManaged})
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":"","source":"managed","can_update":false}`, string(data))
}

func TestManagerBinary(t *testing.T) {
	for _, tt := range []struct {
		name         string
		setupMocks   func(t *testing.T, m *Manager)
		assertResult func(t *testing.T, m *Manager, path string, source BinarySource)
	}{
		{
			name: "system",
			assertResult: func(t *testing.T, _ *Manager, path string, source BinarySource) {
				assert.Equal(t, "yt-dlp", path)
				assert.Equal(t, SourceSystem, source)
			},
		},
		{
			name: "managed",
			setupMocks: func(t *testing.T, m *Manager) {
				require.NoError(t, os.WriteFile(m.managedPath, []byte("binary"), 0o600))
			},
			assertResult: func(t *testing.T, m *Manager, path string, source BinarySource) {
				assert.Equal(t, m.managedPath, path)
				assert.Equal(t, SourceManaged, source)
			},
		},
		{
			name: "external takes precedence over managed",
			setupMocks: func(t *testing.T, m *Manager) {
				require.NoError(t, os.WriteFile(m.managedPath, []byte("binary"), 0o600))
				m.external = "/external/yt-dlp"
			},
			assertResult: func(t *testing.T, m *Manager, path string, source BinarySource) {
				assert.Equal(t, m.external, path)
				assert.Equal(t, SourceExternal, source)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(filepath.Join(t.TempDir(), "yt-dlp"), "")
			if tt.setupMocks != nil {
				tt.setupMocks(t, manager)
			}
			path, source := manager.binary()
			tt.assertResult(t, manager, path, source)
		})
	}
}

func TestDownloadArgs(t *testing.T) {
	base := []string{
		"--ignore-config", "--newline", "--progress", "--no-overwrites",
		"--progress-template", "download:lemontube-progress:%(progress.downloaded_bytes)s:%(progress.total_bytes,progress.total_bytes_estimate)s",
		"--no-simulate", "--print", "before_dl:lemontube-title:%(title)s", "--no-quiet",
		"--embed-metadata", "--js-runtimes", "node", "--remote-components", "ejs:github",
		"--paths", "/work", "--output", "%(title).180B [%(id)s].%(ext)s",
	}
	tests := []struct {
		name         string
		request      Request
		assertResult func(t *testing.T, args []string)
	}{
		{
			name: "default video",
			request: Request{
				Directory:    "/work",
				URL:          "https://example.com",
				DownloadType: "video",
			},
			assertResult: func(t *testing.T, args []string) {
				want := append([]string{}, base...)
				want = append(want, "--format", "bestvideo+bestaudio/best", "--", "https://example.com")
				assert.Equal(t, want, args)
			},
		},
		{
			name: "limited mp4 with subtitles and cookies",
			request: Request{
				Directory:     "/work",
				URL:           "https://example.com",
				DownloadType:  "video",
				Format:        "mp4",
				Quality:       "720",
				SubtitleLangs: []string{"en", "fr"},
				CookiesFile:   "/work/./cookies.txt",
			},
			assertResult: func(t *testing.T, args []string) {
				want := append([]string{}, base...)
				want = append(want,
					"--format", "bestvideo[height<=720]+bestaudio/best[height<=720]",
					"--merge-output-format", "mp4",
					"--write-subs", "--write-auto-subs", "--sub-langs", "en,fr",
					"--cookies", "/work/cookies.txt",
					"--", "https://example.com",
				)
				assert.Equal(t, want, args)
			},
		},
		{
			name: "audio",
			request: Request{
				Directory:    "/work",
				URL:          "https://example.com",
				DownloadType: "audio",
				Format:       "mp3",
			},
			assertResult: func(t *testing.T, args []string) {
				want := append([]string{}, base...)
				want = append(want,
					"--format", "bestaudio/best",
					"--extract-audio", "--audio-format", "mp3",
					"--", "https://example.com",
				)
				assert.Equal(t, want, args)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.assertResult(t, downloadArgs(tt.request))
		})
	}
}

func TestParseProgress(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		percent float64
		ok      bool
	}{
		{
			name:    "half",
			line:    "lemontube-progress:50:100",
			percent: 50,
			ok:      true,
		},
		{
			name:    "clamped above",
			line:    "lemontube-progress:150:100",
			percent: 100,
			ok:      true,
		},
		{
			name: "clamped below",
			line: "lemontube-progress:-1:100",
			ok:   true,
		},
		{
			name: "zero total",
			line: "lemontube-progress:1:0",
		},
		{
			name: "invalid value",
			line: "lemontube-progress:NA:100",
		},
		{
			name: "invalid total",
			line: "lemontube-progress:1:NaN",
		},
		{
			name: "extra field",
			line: "lemontube-progress:1:2:3",
		},
		{
			name: "missing prefix",
			line: "1:2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			percent, ok := parseProgress(tt.line)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.percent, percent)
		})
	}
}

func TestDownloadScannerError(t *testing.T) {
	tests := []struct {
		name         string
		script       string
		assertResult func(t *testing.T, err error)
	}{
		{
			name: "oversized output",
			script: `#!/bin/sh
head -c 1100000 /dev/zero | tr '\0' x
`,
			assertResult: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "read yt-dlp output: bufio.Scanner: token too long")
			},
		},
		{
			name: "preserves cookie and process errors",
			script: `#!/bin/sh
printf 'ERROR: Use --cookies for authentication.\n'
head -c 1100000 /dev/zero | tr '\0' x
exit 7
`,
			assertResult: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "read yt-dlp output: bufio.Scanner: token too long")
				require.ErrorIs(t, err, ErrCookies)
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr)
				assert.Equal(t, 7, exitErr.ExitCode())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "yt-dlp")
			require.NoError(t, os.WriteFile(binary, []byte(tt.script), 0o755))
			tt.assertResult(t, Download(t.Context(), binary, Request{}))
		})
	}
}

func TestDownloadCookieErrors(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		exitCode     int
		assertResult func(t *testing.T, err error)
	}{
		{
			name:     "youtube authentication error",
			line:     "ERROR: [youtube] Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.",
			exitCode: 1,
			assertResult: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrCookies)
			},
		},
		{
			name:     "expired cookies warning and failure",
			line:     "WARNING: Your cookies have expired",
			exitCode: 1,
			assertResult: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrCookies)
			},
		},
		{
			name:     "unrelated failure",
			line:     "ERROR: Video unavailable",
			exitCode: 1,
			assertResult: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "yt-dlp: exit status 1")
				assert.NotContains(t, err.Error(), "cookies")
			},
		},
		{
			name: "cookie warning on successful download",
			line: "WARNING: Your cookies have expired",
			assertResult: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "yt-dlp")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q >&2\nexit %d\n", tt.line, tt.exitCode)
			require.NoError(t, os.WriteFile(binary, []byte(script), 0o755))
			var logs []string
			err := Download(t.Context(), binary, Request{Log: func(line string) {
				logs = append(logs, line)
			}})
			tt.assertResult(t, err)
			assert.Equal(t, []string{tt.line}, logs)
		})
	}
}

func TestDownloadWithInstalledYTDLP(t *testing.T) {
	binary, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp is not installed")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	source := filepath.Join(t.TempDir(), "source.mp4")
	command := exec.CommandContext(t.Context(), ffmpeg,
		"-loglevel", "error", "-f", "lavfi", "-i", "color=black:s=16x16:d=0.2",
		"-c:v", "mpeg4", "-y", source,
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	video, err := os.ReadFile(source)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "video.mp4", time.Time{}, bytes.NewReader(video))
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	var logs []string
	var title string
	err = Download(t.Context(), binary, Request{
		URL:          server.URL + "/video.mp4",
		Directory:    dir,
		DownloadType: "video",
		Format:       "any",
		Quality:      "best",
		Log: func(line string) {
			logs = append(logs, line)
		},
		Title: func(value string) {
			title = value
		},
	})
	require.NoError(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	downloaded := filepath.Join(dir, entries[0].Name())
	metadata, err := exec.CommandContext(t.Context(), ffprobe,
		"-v", "error", "-show_entries", "format_tags=title", "-of", "default=noprint_wrappers=1", downloaded,
	).CombinedOutput()
	require.NoError(t, err, string(metadata))
	assert.Contains(t, string(metadata), "TAG:title=video")
	assert.NotEmpty(t, title)
	assert.NotEmpty(t, logs, "yt-dlp should emit download log messages")
}

func TestAudioDownloadEmbedsMetadata(t *testing.T) {
	binary, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp is not installed")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	source := filepath.Join(t.TempDir(), "source.wav")
	output, err := exec.CommandContext(t.Context(), ffmpeg,
		"-loglevel", "error", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "0.2", "-y", source,
	).CombinedOutput()
	require.NoError(t, err, string(output))
	audio, err := os.ReadFile(source)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		http.ServeContent(w, r, "audio.wav", time.Time{}, bytes.NewReader(audio))
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	err = Download(t.Context(), binary, Request{
		URL:          server.URL + "/audio.wav",
		Directory:    dir,
		DownloadType: "audio",
		Format:       "mp3",
		Quality:      "best",
	})
	require.NoError(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	metadata, err := exec.CommandContext(t.Context(), ffprobe,
		"-v", "error", "-show_entries", "format_tags=title", "-of", "default=noprint_wrappers=1",
		filepath.Join(dir, entries[0].Name()),
	).CombinedOutput()
	require.NoError(t, err, string(metadata))
	assert.Contains(t, string(metadata), "TAG:title=audio")
}

func TestManagerUpdate(t *testing.T) {
	serveBinary := func(t *testing.T, m *Manager, binary string) {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(binary))
		}))
		t.Cleanup(server.Close)
		m.baseURL = server.URL + "/"
	}
	for _, tt := range []struct {
		name         string
		setupMocks   func(t *testing.T, m *Manager)
		assertResult func(t *testing.T, m *Manager, info Info, err error)
	}{
		{
			name: "updates managed binary",
			setupMocks: func(t *testing.T, m *Manager) {
				serveBinary(t, m, "#!/bin/sh\nprintf '2026.11.05\\n'\n")
			},
			assertResult: func(t *testing.T, m *Manager, info Info, err error) {
				require.NoError(t, err)
				assert.Equal(t, "2026.11.05", info.Version)
				assert.Equal(t, SourceManaged, info.Source)
				assert.True(t, info.CanUpdate)
				got, err := m.Version(t.Context())
				require.NoError(t, err)
				assert.Equal(t, info, got)
			},
		},
		{
			name: "rejects external binary",
			setupMocks: func(t *testing.T, m *Manager) {
				m.external = "/external/yt-dlp"
			},
			assertResult: func(t *testing.T, _ *Manager, _ Info, err error) {
				require.ErrorContains(t, err, "externally managed")
			},
		},
		{
			name: "failed update preserves existing binary",
			setupMocks: func(t *testing.T, m *Manager) {
				require.NoError(t, os.WriteFile(m.managedPath, []byte("#!/bin/sh\nprintf 'old-version\\n'\n"), 0o755))
				serveBinary(t, m, "not an executable")
			},
			assertResult: func(t *testing.T, m *Manager, _ Info, err error) {
				require.Error(t, err)
				info, err := m.Version(t.Context())
				require.NoError(t, err)
				assert.Equal(t, "old-version", info.Version)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(filepath.Join(t.TempDir(), "yt-dlp"), "")
			tt.setupMocks(t, manager)
			info, err := manager.Update(t.Context())
			tt.assertResult(t, manager, info, err)
		})
	}
}

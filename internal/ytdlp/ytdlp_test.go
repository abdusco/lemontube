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
	for _, source := range []BinarySource{SourceExternal, SourceManaged, SourceSystem} {
		assert.NoError(t, source.Validate())
	}
	assert.Error(t, BinarySource("unknown").Validate())
	data, err := json.Marshal(Info{Source: SourceManaged})
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":"","source":"managed","can_update":false}`, string(data))
}

func TestManagerBinary(t *testing.T) {
	managed := filepath.Join(t.TempDir(), "yt-dlp")
	manager := NewManager(managed, "")
	path, source := manager.binary()
	assert.Equal(t, "yt-dlp", path)
	assert.Equal(t, SourceSystem, source)
	require.NoError(t, os.WriteFile(managed, []byte("binary"), 0o600))
	path, source = manager.binary()
	assert.Equal(t, managed, path)
	assert.Equal(t, SourceManaged, source)
	manager.external = "/external/yt-dlp"
	path, source = manager.binary()
	assert.Equal(t, manager.external, path)
	assert.Equal(t, SourceExternal, source)
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
		name    string
		request Request
		options []string
	}{
		{"default video", Request{Directory: "/work", URL: "https://example.com", DownloadType: "video"}, []string{"--format", "bestvideo+bestaudio/best"}},
		{"limited mp4 with subtitles and cookies", Request{Directory: "/work", URL: "https://example.com", DownloadType: "video", Format: "mp4", Quality: "720", SubtitleLangs: []string{"en", "fr"}, CookiesFile: "/work/./cookies.txt"}, []string{"--format", "bestvideo[height<=720]+bestaudio/best[height<=720]", "--merge-output-format", "mp4", "--write-subs", "--write-auto-subs", "--sub-langs", "en,fr", "--cookies", "/work/cookies.txt"}},
		{"audio", Request{Directory: "/work", URL: "https://example.com", DownloadType: "audio", Format: "mp3"}, []string{"--format", "bestaudio/best", "--extract-audio", "--audio-format", "mp3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := append([]string{}, base...)
			want = append(want, tt.options...)
			want = append(want, "--", tt.request.URL)
			assert.Equal(t, want, downloadArgs(tt.request))
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
		{"half", "lemontube-progress:50:100", 50, true},
		{"clamped above", "lemontube-progress:150:100", 100, true},
		{"clamped below", "lemontube-progress:-1:100", 0, true},
		{"zero total", "lemontube-progress:1:0", 0, false},
		{"invalid value", "lemontube-progress:NA:100", 0, false},
		{"invalid total", "lemontube-progress:1:NaN", 0, false},
		{"extra field", "lemontube-progress:1:2:3", 0, false},
		{"missing prefix", "1:2", 0, false},
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
			name:   "oversized output",
			script: "#!/bin/sh\nhead -c 1100000 /dev/zero | tr '\\0' x\n",
			assertResult: func(t *testing.T, err error) {
				require.ErrorContains(t, err, "read yt-dlp output: bufio.Scanner: token too long")
			},
		},
		{
			name:   "preserves cookie and process errors",
			script: "#!/bin/sh\nprintf 'ERROR: Use --cookies for authentication.\\n'\nhead -c 1100000 /dev/zero | tr '\\0' x\nexit 7\n",
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\nprintf '2026.11.05\\n'\n"))
	}))
	t.Cleanup(server.Close)
	manager := NewManager(filepath.Join(t.TempDir(), "bin", "yt-dlp"), "")
	manager.baseURL = server.URL + "/"
	info, err := manager.Update(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "2026.11.05", info.Version)
	assert.Equal(t, SourceManaged, info.Source)
	assert.True(t, info.CanUpdate)
	got, err := manager.Version(t.Context())
	require.NoError(t, err)
	assert.Equal(t, info, got)
}

func TestManagerRejectsExternalUpdate(t *testing.T) {
	manager := NewManager(filepath.Join(t.TempDir(), "managed"), "/external/yt-dlp")
	_, err := manager.Update(t.Context())
	require.ErrorContains(t, err, "externally managed")
}

func TestFailedUpdatePreservesExistingBinary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not an executable"))
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "yt-dlp")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nprintf 'old-version\\n'\n"), 0o755))
	manager := NewManager(path, "")
	manager.baseURL = server.URL + "/"
	_, err := manager.Update(t.Context())
	require.Error(t, err)
	info, err := manager.Version(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "old-version", info.Version)
}

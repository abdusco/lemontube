package internal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStatusIsActive(t *testing.T) {
	for _, tt := range []struct {
		name     string
		value    JobStatus
		expected bool
	}{
		{
			name:     "queued",
			value:    JobQueued,
			expected: true,
		},
		{
			name:     "running",
			value:    JobRunning,
			expected: true,
		},
		{
			name:  "finished",
			value: JobFinished,
		},
		{
			name:  "failed",
			value: JobFailed,
		},
		{
			name:  "canceled",
			value: JobCanceled,
		},
		{
			name:  "invalid",
			value: "invalid",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.value.IsActive())
		})
	}
}

func TestNewJobID(t *testing.T) {
	id, err := NewJobID()
	require.NoError(t, err)
	require.NoError(t, id.Validate())
	assert.Len(t, id, 32)
}

func TestJobIDValidate(t *testing.T) {
	for _, tt := range []struct {
		name         string
		id           JobID
		assertResult func(t *testing.T, err error)
	}{
		{
			name: "empty",
			assertResult: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "path traversal",
			id:   "../other",
			assertResult: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "invalid hex",
			id:   "0000000000000000000000000000000z",
			assertResult: func(t *testing.T, err error) {
				require.Error(t, err)
			},
		},
		{
			name: "mixed case hex",
			id:   "ABCDEF0123456789abcdef0123456789",
			assertResult: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.assertResult(t, tt.id.Validate())
		})
	}
}

func TestCookiesFormatting(t *testing.T) {
	tests := []struct {
		name    string
		cookies Cookies
		domains []string
		text    string
	}{
		{
			name:    "empty",
			domains: []string{},
			text: `# Netscape HTTP Cookie File

`,
		},
		{
			name: "sorted domains",
			cookies: Cookies{
				"z.example": "z row",
				"a.example": `a row
second row`,
			},
			domains: []string{"a.example", "z.example"},
			text: `# Netscape HTTP Cookie File
a row
second row
z row
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.domains, tt.cookies.Domains())
			assert.Equal(t, tt.text, tt.cookies.NetscapeTXT())
		})
	}
}

func TestOutputEnums(t *testing.T) {
	t.Run("kind", func(t *testing.T) {
		assert.NoError(t, OutputMedia.Validate())
		assert.NoError(t, OutputSubtitle.Validate())
		assert.Error(t, OutputKind("unknown").Validate())
	})
	t.Run("state", func(t *testing.T) {
		assert.NoError(t, OutputPublishing.Validate())
		assert.NoError(t, OutputPublished.Validate())
		assert.Error(t, OutputState("unknown").Validate())
	})
}

func TestOutputJSONRoundTrip(t *testing.T) {
	output := OutputFile{Path: "video.mp4", Kind: OutputMedia, State: OutputPublished}
	data, err := json.Marshal(output)
	require.NoError(t, err)
	assert.JSONEq(t, `{"path":"video.mp4","kind":"media","state":"published"}`, string(data))
	var decoded OutputFile
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, output, decoded)
}

func TestDownloadOptionsNormalize(t *testing.T) {
	tests := []struct {
		name         string
		kind         DownloadType
		options      DownloadOptions
		assertResult func(t *testing.T, options DownloadOptions, err error)
	}{
		{
			name: "video defaults",
			kind: DownloadVideo,
			assertResult: func(t *testing.T, options DownloadOptions, err error) {
				require.NoError(t, err)
				assert.Equal(t, DownloadOptions{Format: "mp4", Quality: "best"}, options)
			},
		},
		{
			name: "audio defaults",
			kind: DownloadAudio,
			assertResult: func(t *testing.T, options DownloadOptions, err error) {
				require.NoError(t, err)
				assert.Equal(t, DownloadOptions{Format: "mp3", Quality: "best"}, options)
			},
		},
		{
			name: "explicit options",
			kind: DownloadVideo,
			options: DownloadOptions{
				Format:        "any",
				Quality:       "720",
				SubtitleLangs: []string{"en", "fr"},
			},
			assertResult: func(t *testing.T, options DownloadOptions, err error) {
				require.NoError(t, err)
				assert.Equal(t, DownloadOptions{Format: "any", Quality: "720", SubtitleLangs: []string{"en", "fr"}}, options)
			},
		},
		{
			name: "invalid type",
			kind: "invalid",
			assertResult: func(t *testing.T, _ DownloadOptions, err error) {
				require.ErrorContains(t, err, "invalid download type")
			},
		},
		{
			name:    "invalid audio format",
			kind:    DownloadAudio,
			options: DownloadOptions{Format: "mp4"},
			assertResult: func(t *testing.T, _ DownloadOptions, err error) {
				require.ErrorContains(t, err, "invalid audio format")
			},
		},
		{
			name:    "invalid video format",
			kind:    DownloadVideo,
			options: DownloadOptions{Format: "mp3"},
			assertResult: func(t *testing.T, _ DownloadOptions, err error) {
				require.ErrorContains(t, err, "invalid video format")
			},
		},
		{
			name:    "invalid quality",
			kind:    DownloadVideo,
			options: DownloadOptions{Quality: "10001"},
			assertResult: func(t *testing.T, _ DownloadOptions, err error) {
				require.ErrorContains(t, err, "invalid quality")
			},
		},
		{
			name:    "invalid language",
			kind:    DownloadVideo,
			options: DownloadOptions{SubtitleLangs: []string{"en,fr"}},
			assertResult: func(t *testing.T, _ DownloadOptions, err error) {
				require.ErrorContains(t, err, "invalid subtitle language")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := tt.options.Normalize(tt.kind)
			tt.assertResult(t, options, err)
		})
	}
}

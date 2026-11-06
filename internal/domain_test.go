package internal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStatusIsActive(t *testing.T) {
	for _, status := range []JobStatus{JobQueued, JobRunning} {
		assert.True(t, status.IsActive(), "%s", status)
	}
	for _, status := range []JobStatus{JobFinished, JobFailed, JobCanceled, "invalid"} {
		assert.False(t, status.IsActive(), "%s", status)
	}
}

func TestJobID(t *testing.T) {
	id, err := NewJobID()
	require.NoError(t, err)
	require.NoError(t, id.Validate())
	assert.Len(t, id, 32)
	for _, invalid := range []JobID{"", "../other", "0000000000000000000000000000000z"} {
		assert.Error(t, invalid.Validate())
	}
	assert.NoError(t, JobID("ABCDEF0123456789abcdef0123456789").Validate())
}

func TestCookiesFormatting(t *testing.T) {
	tests := []struct {
		name    string
		cookies Cookies
		domains []string
		text    string
	}{
		{"empty", nil, []string{}, "# Netscape HTTP Cookie File\n\n"},
		{"sorted domains", Cookies{"z.example": "z row", "a.example": "a row\nsecond row"}, []string{"a.example", "z.example"}, "# Netscape HTTP Cookie File\na row\nsecond row\nz row\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.domains, tt.cookies.Domains())
			assert.Equal(t, tt.text, tt.cookies.NetscapeTXT())
		})
	}
}

func TestOutputEnums(t *testing.T) {
	for _, kind := range []OutputKind{OutputMedia, OutputSubtitle} {
		assert.NoError(t, kind.Validate())
	}
	assert.Error(t, OutputKind("unknown").Validate())
	for _, state := range []OutputState{OutputPublishing, OutputPublished} {
		assert.NoError(t, state.Validate())
	}
	assert.Error(t, OutputState("unknown").Validate())

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
		{"video defaults", DownloadVideo, DownloadOptions{}, func(t *testing.T, options DownloadOptions, err error) {
			require.NoError(t, err)
			assert.Equal(t, DownloadOptions{Format: "mp4", Quality: "best"}, options)
		}},
		{"audio defaults", DownloadAudio, DownloadOptions{}, func(t *testing.T, options DownloadOptions, err error) {
			require.NoError(t, err)
			assert.Equal(t, DownloadOptions{Format: "mp3", Quality: "best"}, options)
		}},
		{"explicit options", DownloadVideo, DownloadOptions{Format: "any", Quality: "720", SubtitleLangs: []string{"en", "fr"}}, func(t *testing.T, options DownloadOptions, err error) {
			require.NoError(t, err)
			assert.Equal(t, DownloadOptions{Format: "any", Quality: "720", SubtitleLangs: []string{"en", "fr"}}, options)
		}},
		{"invalid type", "invalid", DownloadOptions{}, func(t *testing.T, _ DownloadOptions, err error) {
			require.ErrorContains(t, err, "invalid download type")
		}},
		{"invalid audio format", DownloadAudio, DownloadOptions{Format: "mp4"}, func(t *testing.T, _ DownloadOptions, err error) {
			require.ErrorContains(t, err, "invalid audio format")
		}},
		{"invalid video format", DownloadVideo, DownloadOptions{Format: "mp3"}, func(t *testing.T, _ DownloadOptions, err error) {
			require.ErrorContains(t, err, "invalid video format")
		}},
		{"invalid quality", DownloadVideo, DownloadOptions{Quality: "10001"}, func(t *testing.T, _ DownloadOptions, err error) {
			require.ErrorContains(t, err, "invalid quality")
		}},
		{"invalid language", DownloadVideo, DownloadOptions{SubtitleLangs: []string{"en,fr"}}, func(t *testing.T, _ DownloadOptions, err error) {
			require.ErrorContains(t, err, "invalid subtitle language")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := tt.options.Normalize(tt.kind)
			tt.assertResult(t, options, err)
		})
	}
}

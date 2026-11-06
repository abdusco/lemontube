package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

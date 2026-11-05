package internal

import "fmt"

type JobStatus string

const (
	JobQueued   JobStatus = "queued"
	JobRunning  JobStatus = "running"
	JobFinished JobStatus = "finished"
	JobFailed   JobStatus = "failed"
	JobCanceled JobStatus = "canceled"
)

func (s JobStatus) Validate() error {
	switch s {
	case JobQueued, JobRunning, JobFinished, JobFailed, JobCanceled:
		return nil
	default:
		return fmt.Errorf("invalid job status %q", s)
	}
}

type DownloadType string

const (
	DownloadVideo DownloadType = "video"
	DownloadAudio DownloadType = "audio"
)

func (t DownloadType) Validate() error {
	switch t {
	case DownloadVideo, DownloadAudio:
		return nil
	default:
		return fmt.Errorf("invalid download type %q", t)
	}
}

type DownloadOptions struct {
	Codec         string   `json:"codec"`
	Format        string   `json:"format"`
	Quality       string   `json:"quality"`
	SubtitleLangs []string `json:"subtitle_langs"`
}

type OutputFile struct {
	Path     string  `json:"path"`
	Kind     string  `json:"kind"`
	State    string  `json:"state"`
	Size     *int64  `json:"size,omitempty"`
	Language *string `json:"language,omitempty"`
	Stage    string  `json:"stage,omitempty"`
	SHA256   string  `json:"sha256,omitempty"`
}

type Job struct {
	ID                string          `json:"id"`
	URL               string          `json:"url"`
	Title             string          `json:"title"`
	DownloadType      DownloadType    `json:"download_type"`
	Options           DownloadOptions `json:"options"`
	Status            JobStatus       `json:"status"`
	Outputs           []OutputFile    `json:"outputs"`
	Message           *string         `json:"message"`
	Error             *string         `json:"error"`
	Percent           *float64        `json:"percent"`
	DeleteRequestedAt *string         `json:"-"`
	DeleteOutputs     bool            `json:"-"`
	CleanupError      *string         `json:"cleanup_error,omitempty"`
	CleanupAfter      *string         `json:"-"`
	CreatedAt         string          `json:"created_at"`
	FinishedAt        *string         `json:"finished_at"`
}

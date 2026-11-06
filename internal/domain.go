package internal

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

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

func (o DownloadOptions) Normalize(kind DownloadType) (DownloadOptions, error) {
	if err := kind.Validate(); err != nil {
		return o, err
	}
	if o.Format == "" {
		if kind == DownloadAudio {
			o.Format = "mp3"
		} else {
			o.Format = "mp4"
		}
	}
	if kind == DownloadAudio {
		switch o.Format {
		case "mp3", "m4a", "opus", "wav", "flac":
		default:
			return o, errors.New("invalid audio format")
		}
	} else if o.Format != "mp4" && o.Format != "any" {
		return o, errors.New("invalid video format")
	}
	if o.Quality == "" {
		o.Quality = "best"
	}
	if o.Quality != "best" {
		quality, err := strconv.Atoi(o.Quality)
		if err != nil || quality < 1 || quality > 10000 {
			return o, errors.New("invalid quality")
		}
	}
	for _, lang := range o.SubtitleLangs {
		if lang == "" || strings.ContainsAny(lang, ",/\\ \n\r") {
			return o, errors.New("invalid subtitle language")
		}
	}
	return o, nil
}

type OutputKind string

const (
	OutputMedia    OutputKind = "media"
	OutputSubtitle OutputKind = "subtitle"
)

func (k OutputKind) Validate() error {
	switch k {
	case OutputMedia, OutputSubtitle:
		return nil
	default:
		return fmt.Errorf("invalid output kind %q", k)
	}
}

type OutputState string

const (
	OutputPublishing OutputState = "publishing"
	OutputPublished  OutputState = "published"
)

func (s OutputState) Validate() error {
	switch s {
	case OutputPublishing, OutputPublished:
		return nil
	default:
		return fmt.Errorf("invalid output state %q", s)
	}
}

type OutputFile struct {
	Path     string      `json:"path"`
	Kind     OutputKind  `json:"kind"`
	State    OutputState `json:"state"`
	Size     *int64      `json:"size,omitempty"`
	Language *string     `json:"language,omitempty"`
	Stage    string      `json:"stage,omitempty"`
	SHA256   string      `json:"sha256,omitempty"`
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
	CleanupComplete   bool            `json:"-"`
	CleanupError      *string         `json:"cleanup_error,omitempty"`
	CleanupAfter      *string         `json:"-"`
	CreatedAt         string          `json:"created_at"`
	FinishedAt        *string         `json:"finished_at"`
}

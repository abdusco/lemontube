package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const nightlyURL = "https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/"

// Manager coordinates downloads with updates to a persistent standalone binary.
type Manager struct {
	managedPath string
	external    string
	baseURL     string
	client      *http.Client
	mutex       sync.RWMutex
}

type Info struct {
	Version   string `json:"version"`
	Source    string `json:"source"`
	CanUpdate bool   `json:"can_update"`
}

func NewManager(managedPath, external string) *Manager {
	return &Manager{
		managedPath: managedPath,
		external:    external,
		baseURL:     nightlyURL,
		client:      &http.Client{Timeout: 5 * time.Minute},
	}
}

func (m *Manager) binary() (string, string) {
	if m.external != "" {
		return m.external, "external"
	}
	if info, err := os.Stat(m.managedPath); err == nil && info.Mode().IsRegular() {
		return m.managedPath, "managed"
	}
	return "yt-dlp", "system"
}

func (m *Manager) Version(ctx context.Context) (Info, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	path, source := m.binary()
	output, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return Info{Source: source, CanUpdate: m.external == ""}, fmt.Errorf("yt-dlp version: %w", err)
	}
	return Info{Version: strings.TrimSpace(string(output)), Source: source, CanUpdate: m.external == ""}, nil
}

func assetName(goos, arch string) (string, error) {
	if goos == "darwin" && (arch == "arm64" || arch == "amd64") {
		return "yt-dlp_macos", nil
	}
	if goos == "linux" && (arch == "arm64" || arch == "amd64") {
		name := "yt-dlp_linux"
		matches, _ := filepath.Glob("/lib/ld-musl-*")
		if len(matches) > 0 {
			name = "yt-dlp_musllinux"
		}
		if arch == "arm64" {
			name += "_aarch64"
		}
		return name, nil
	}
	return "", fmt.Errorf("no yt-dlp build for %s/%s", goos, arch)
}

// Update downloads and validates a nightly binary before replacing the managed copy.
func (m *Manager) Update(ctx context.Context) (Info, error) {
	if m.external != "" {
		return Info{}, errors.New("YTDLP_PATH is externally managed; update it yourself")
	}
	asset, err := assetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Info{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+asset, nil)
	if err != nil {
		return Info{}, err
	}
	response, err := m.client.Do(request)
	if err != nil {
		return Info{}, fmt.Errorf("download yt-dlp: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("download yt-dlp: HTTP %d", response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(m.managedPath), 0o755); err != nil {
		return Info{}, err
	}
	file, err := os.CreateTemp(filepath.Dir(m.managedPath), ".yt-dlp-*")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(file.Name())
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 200<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return Info{}, fmt.Errorf("download yt-dlp: %w", copyErr)
	}
	if closeErr != nil {
		return Info{}, closeErr
	}
	if err := os.Chmod(file.Name(), 0o755); err != nil {
		return Info{}, err
	}
	output, err := exec.CommandContext(ctx, file.Name(), "--version").Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return Info{}, errors.New("downloaded yt-dlp failed its version check")
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if err := os.Rename(file.Name(), m.managedPath); err != nil {
		return Info{}, err
	}
	return Info{Version: strings.TrimSpace(string(output)), Source: "managed", CanUpdate: true}, nil
}

func (m *Manager) Download(ctx context.Context, request Request) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	path, _ := m.binary()
	return Download(ctx, path, request)
}

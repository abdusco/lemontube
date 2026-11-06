package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lemontube/internal/ytdlp"
)

type Config struct {
	StateDir      string
	DownloadDir   string
	TempDir       string
	YTDLPPath     string
	Concurrency   int
	DeleteOutputs bool
	ClearAfter    time.Duration
}

type Service struct {
	repo        *Repository
	config      Config
	logger      *slog.Logger
	mutex       sync.Mutex
	orphanMutex sync.Mutex
	running     map[string]context.CancelFunc
	wg          sync.WaitGroup
	wake        chan struct{}
	ytdlp       *ytdlp.Manager
}

func NewService(repo *Repository, config Config, logger *slog.Logger) (*Service, error) {
	if config.Concurrency < 1 {
		config.Concurrency = 3
	}
	if config.StateDir == "" {
		config.StateDir = config.TempDir
	}
	for _, dir := range []string{config.DownloadDir, filepath.Join(config.TempDir, ".lemontube")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return &Service{
		repo:    repo,
		config:  config,
		logger:  logger,
		running: make(map[string]context.CancelFunc),
		wake:    make(chan struct{}, 1),
		ytdlp:   ytdlp.NewManager(filepath.Join(config.StateDir, "bin", "yt-dlp"), config.YTDLPPath),
	}, nil
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Enqueue(ctx context.Context, rawURL string, downloadType DownloadType, options DownloadOptions) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("a valid HTTP(S) URL is required")
	}
	options, err = options.Normalize(downloadType)
	if err != nil {
		return "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(random[:])
	job := Job{
		ID:           id,
		URL:          rawURL,
		Title:        rawURL,
		DownloadType: downloadType,
		Options:      options,
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return "", err
	}
	s.signal()
	return id, nil
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.repo.RecoverJobs(ctx); err != nil {
		return err
	}
	if err := s.sweep(ctx); err != nil {
		s.logger.Error("startup cleanup", "error", err)
	}
	s.wg.Add(1)
	go s.loop(ctx)
	return nil
}

func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) loop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.sweep(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("cleanup", "error", err)
		}
		s.schedule(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

func (s *Service) schedule(ctx context.Context) {
	jobs, err := s.repo.ListJobs(ctx, false)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("list queue", "error", err)
		}
		return
	}
	for _, job := range jobs {
		if job.Status != JobQueued {
			continue
		}
		s.mutex.Lock()
		if len(s.running) >= s.config.Concurrency {
			s.mutex.Unlock()
			return
		}
		claimed, err := s.repo.ClaimJob(ctx, job.ID)
		if err != nil || !claimed {
			s.mutex.Unlock()
			continue
		}
		jobCtx, cancel := context.WithCancel(ctx)
		s.running[job.ID] = cancel
		s.wg.Add(1)
		s.mutex.Unlock()
		go s.run(jobCtx, job)
	}
}

func (s *Service) run(ctx context.Context, job Job) {
	defer s.wg.Done()
	defer func() {
		s.mutex.Lock()
		delete(s.running, job.ID)
		s.mutex.Unlock()
		s.signal()
	}()
	workspace := filepath.Join(s.config.TempDir, ".lemontube", job.ID)
	err := os.Mkdir(workspace, 0o700)
	if err == nil {
		_, cookies, cookieErr := s.repo.Cookies(ctx)
		if cookieErr != nil {
			err = cookieErr
		}
		cookieFile := ""
		if cookieErr == nil && strings.TrimSpace(cookies) != "# Netscape HTTP Cookie File" {
			cookieFile = filepath.Join(workspace, ".cookies.txt")
			err = os.WriteFile(cookieFile, []byte(cookies), 0o600)
		}
		if err == nil {
			lastPercent := -1
			titleSet := false
			err = s.ytdlp.Download(ctx, ytdlp.Request{
				URL:           job.URL,
				Directory:     workspace,
				DownloadType:  string(job.DownloadType),
				Format:        job.Options.Format,
				Quality:       job.Options.Quality,
				SubtitleLangs: job.Options.SubtitleLangs,
				CookiesFile:   cookieFile,
				Title: func(title string) {
					if titleSet || title == "" {
						return
					}
					titleSet = true
					if err := s.repo.SetTitle(context.Background(), job.ID, title); err != nil {
						s.logger.Warn("save title", "error", err)
					}
				},
				Progress: func(percent float64) {
					whole := int(percent)
					if whole == lastPercent {
						return
					}
					lastPercent = whole
					if err := s.repo.SetProgress(context.Background(), job.ID, percent); err != nil {
						s.logger.Warn("save progress", "error", err)
					}
				},
				Log: func(line string) {
					if err := s.repo.AddLog(context.Background(), job.ID, line); err != nil {
						s.logger.Warn("save log", "error", err)
					}
				},
			})
		}
	}
	if err == nil {
		err = s.publish(ctx, job.ID, workspace)
	}
	status := JobFinished
	var message *string
	if err != nil {
		status = JobFailed
		text := err.Error()
		if errors.Is(err, ytdlp.ErrCookies) {
			text = "Site authentication failed. Add or update the site's cookies in Site cookies, then retry."
			if submittedURL, parseErr := url.Parse(job.URL); parseErr == nil && submittedURL.Hostname() != "" {
				text = fmt.Sprintf("Site authentication failed. Add or update cookies for %s in Site cookies, then retry.", submittedURL.Hostname())
			}
		}
		message = &text
		if ctx.Err() != nil {
			status = JobCanceled
		}
	}
	if err := s.repo.FinishJob(context.Background(), job.ID, status, message); err != nil {
		s.logger.Error("finish job", "error", err)
	}
	if err := os.RemoveAll(workspace); err != nil {
		s.logger.Warn("remove workspace", "job", job.ID, "error", err)
	}
}

func (s *Service) Delete(ctx context.Context, id string) error {
	found, err := s.repo.RequestDelete(ctx, id, s.config.DeleteOutputs)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("job not found")
	}
	s.mutex.Lock()
	if cancel := s.running[id]; cancel != nil {
		cancel()
	}
	s.mutex.Unlock()
	s.signal()
	return nil
}

func (s *Service) Jobs(ctx context.Context) ([]Job, error) {
	return s.repo.ListJobs(ctx, false)
}

func (s *Service) Logs(ctx context.Context, id string) ([]string, error) {
	return s.repo.Logs(ctx, id)
}

func (s *Service) YTDLPVersion(ctx context.Context) (ytdlp.Info, error) {
	return s.ytdlp.Version(ctx)
}

func (s *Service) UpdateYTDLP(ctx context.Context) (ytdlp.Info, error) {
	return s.ytdlp.Update(ctx)
}

func (s *Service) Retry(ctx context.Context, id string) (string, error) {
	job, err := s.repo.GetJob(ctx, id)
	if err != nil {
		return "", err
	}
	if job.Status == JobQueued || job.Status == JobRunning {
		return "", errors.New("job is still active")
	}
	return s.Enqueue(ctx, job.URL, job.DownloadType, job.Options)
}

func (s *Service) Cookies(ctx context.Context) ([]string, error) {
	domains, _, err := s.repo.Cookies(ctx)
	return domains, err
}

func (s *Service) SaveCookies(ctx context.Context, text string) error {
	if len(text) > 1_000_000 {
		return errors.New("cookie file exceeds 1 MB")
	}
	cookies, err := parseCookiesTXT(text)
	if err != nil {
		return err
	}
	return s.repo.PutCookies(ctx, cookies)
}

func parseCookiesTXT(text string) (map[string]string, error) {
	byDomain := make(map[string][]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		parsed := line
		if strings.HasPrefix(line, "#HttpOnly_") {
			parsed = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(parsed, "\t")
		if len(fields) != 7 || fields[0] == "" {
			continue
		}
		byDomain[fields[0]] = append(byDomain[fields[0]], line)
	}
	if len(byDomain) == 0 {
		return nil, errors.New("no valid cookies found")
	}
	cookies := make(map[string]string, len(byDomain))
	for domain, lines := range byDomain {
		cookies[domain] = strings.Join(lines, "\n")
	}
	return cookies, nil
}

func (s *Service) DeleteCookies(ctx context.Context, domain string) error {
	return s.repo.DeleteCookies(ctx, domain)
}

// DownloadPath only exposes recorded published files, not state or workspaces.
func (s *Service) DownloadPath(ctx context.Context, name string) (string, error) {
	if name == "." || filepath.Base(name) != name {
		return "", fs.ErrNotExist
	}
	jobs, err := s.repo.ListJobs(ctx, false)
	if err != nil {
		return "", err
	}
	for _, job := range jobs {
		for _, output := range job.Outputs {
			if output.Path != name || output.State != OutputPublished {
				continue
			}
			path := filepath.Join(s.config.DownloadDir, name)
			info, err := os.Lstat(path)
			if err != nil {
				return "", err
			}
			if !info.Mode().IsRegular() {
				return "", fs.ErrNotExist
			}
			return path, nil
		}
	}
	return "", fs.ErrNotExist
}

package ytdlp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrCookies = errors.New("yt-dlp reported a cookie authentication problem")

// Request contains the options needed to run yt-dlp in an isolated workspace.
type Request struct {
	URL           string
	Directory     string
	DownloadType  string
	Format        string
	Quality       string
	SubtitleLangs []string
	CookiesFile   string
	Log           func(string)
	Progress      func(float64)
	Title         func(string)
}

// Download runs yt-dlp and waits for the whole process to exit on cancellation.
func Download(ctx context.Context, binary string, request Request) error {
	args := []string{
		"--ignore-config", "--newline", "--progress", "--no-overwrites",
		"--progress-template", "download:lemontube-progress:%(progress.downloaded_bytes)s:%(progress.total_bytes,progress.total_bytes_estimate)s",
		"--no-simulate", "--print", "before_dl:lemontube-title:%(title)s", "--no-quiet",
		"--embed-metadata",
		"--js-runtimes", "node", "--remote-components", "ejs:github",
		"--paths", request.Directory,
		"--output", "%(title).180B [%(id)s].%(ext)s",
	}
	if request.DownloadType == "audio" {
		args = append(args, "--format", "bestaudio/best", "--extract-audio", "--audio-format", request.Format)
	} else {
		selector := "bestvideo+bestaudio/best"
		if request.Quality != "" && request.Quality != "best" {
			selector = "bestvideo[height<=" + request.Quality + "]+bestaudio/best[height<=" + request.Quality + "]"
		}
		args = append(args, "--format", selector)
		if request.Format == "mp4" {
			args = append(args, "--merge-output-format", "mp4")
		}
	}
	if len(request.SubtitleLangs) > 0 {
		args = append(args, "--write-subs", "--write-auto-subs", "--sub-langs", strings.Join(request.SubtitleLangs, ","))
	}
	if request.CookiesFile != "" {
		args = append(args, "--cookies", filepath.Clean(request.CookiesFile))
	}
	args = append(args, "--", request.URL)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	done := make(chan struct{})
	cookieProblem := false
	go func() {
		defer close(done)
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			if isCookieProblem(scanner.Text()) {
				cookieProblem = true
			}
			if strings.HasPrefix(scanner.Text(), "lemontube-title:") {
				if request.Title != nil {
					request.Title(strings.TrimPrefix(scanner.Text(), "lemontube-title:"))
				}
				continue
			}
			if strings.HasPrefix(scanner.Text(), "lemontube-progress:") {
				parts := strings.Split(strings.TrimPrefix(scanner.Text(), "lemontube-progress:"), ":")
				if len(parts) == 2 && request.Progress != nil {
					downloaded, firstErr := strconv.ParseFloat(parts[0], 64)
					total, secondErr := strconv.ParseFloat(parts[1], 64)
					if firstErr == nil && secondErr == nil && total > 0 {
						request.Progress(min(100, max(0, downloaded/total*100)))
					}
				}
				continue
			}
			if request.Log != nil {
				request.Log(scanner.Text())
			}
		}
	}()
	err := cmd.Run()
	writer.Close()
	<-done
	reader.Close()
	if err != nil {
		if ctx.Err() == nil && cookieProblem {
			return fmt.Errorf("%w: %w", ErrCookies, err)
		}
		return fmt.Errorf("yt-dlp: %w", err)
	}
	return nil
}

func isCookieProblem(line string) bool {
	line = strings.ToLower(line)
	if !strings.HasPrefix(line, "error:") && !strings.HasPrefix(line, "warning:") {
		return false
	}
	if strings.HasPrefix(line, "error:") && (strings.Contains(line, "--cookies") || strings.Contains(line, "cookies")) {
		return true
	}
	return strings.Contains(line, "cookies") && (strings.Contains(line, "expired") || strings.Contains(line, "no longer valid"))
}

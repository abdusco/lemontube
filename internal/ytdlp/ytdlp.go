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

func downloadArgs(request Request) []string {
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
	return append(args, "--", request.URL)
}

// Download runs yt-dlp and waits for the whole process to exit on cancellation.
func Download(ctx context.Context, binary string, request Request) error {
	cmd := exec.CommandContext(ctx, binary, downloadArgs(request)...)
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
	type scanResult struct {
		cookieProblem bool
		err           error
	}
	done := make(chan scanResult, 1)
	go func() {
		defer reader.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		cookieProblem := false
		for scanner.Scan() {
			line := scanner.Text()
			if isCookieProblem(line) {
				cookieProblem = true
			}
			if title, ok := strings.CutPrefix(line, "lemontube-title:"); ok {
				if request.Title != nil {
					request.Title(title)
				}
				continue
			}
			if strings.HasPrefix(line, "lemontube-progress:") {
				if percent, ok := parseProgress(line); ok && request.Progress != nil {
					request.Progress(percent)
				}
				continue
			}
			if request.Log != nil {
				request.Log(line)
			}
		}
		done <- scanResult{cookieProblem: cookieProblem, err: scanner.Err()}
	}()
	err := cmd.Run()
	writer.Close()
	result := <-done
	if err != nil {
		if ctx.Err() == nil && result.cookieProblem {
			err = fmt.Errorf("%w: %w", ErrCookies, err)
		} else {
			err = fmt.Errorf("yt-dlp: %w", err)
		}
	}
	if result.err != nil {
		return errors.Join(err, fmt.Errorf("read yt-dlp output: %w", result.err))
	}
	return err
}

func parseProgress(line string) (float64, bool) {
	progress, ok := strings.CutPrefix(line, "lemontube-progress:")
	if !ok {
		return 0, false
	}
	parts := strings.Split(progress, ":")
	if len(parts) != 2 {
		return 0, false
	}
	downloaded, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, false
	}
	total, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || !(total > 0) {
		return 0, false
	}
	return min(100, max(0, downloaded/total*100)), true
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

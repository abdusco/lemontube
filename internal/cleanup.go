package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// publish records intent before creating a staging file. A hard link provides
// an atomic, no-overwrite finalization and lets recovery prove file ownership.
func (s *Service) publish(ctx context.Context, id, workspace string) error {
	var paths []string
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected non-regular file in workspace")
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".ytdl") || strings.HasPrefix(name, ".") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("yt-dlp produced no output files")
	}
	outputs := make([]OutputFile, 0, len(paths))
	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.Base(path)
		stage := fmt.Sprintf(".lemontube-%s-%d", id, index)
		stagePath := filepath.Join(s.config.DownloadDir, stage)
		destination := name
		for suffix := 0; ; suffix++ {
			if suffix > 0 {
				destination = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, filepath.Ext(name)), suffix, filepath.Ext(name))
			}
			if _, err := os.Lstat(filepath.Join(s.config.DownloadDir, destination)); errors.Is(err, os.ErrNotExist) {
				break
			} else if err != nil {
				return err
			}
		}
		kind := OutputMedia
		switch strings.ToLower(filepath.Ext(name)) {
		case ".vtt", ".srt", ".ass", ".ttml":
			kind = OutputSubtitle
		}
		outputs = append(outputs, OutputFile{
			Path:  destination,
			Kind:  kind,
			State: OutputPublishing,
			Stage: stage,
		})
		if err := s.repo.SetOutputs(ctx, id, outputs); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(stagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			input.Close()
			return err
		}
		hash := sha256.New()
		size, copyErr := io.Copy(io.MultiWriter(output, hash), input)
		input.Close()
		if copyErr == nil {
			copyErr = output.Sync()
		}
		if closeErr := output.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return copyErr
		}
		outputs[index].Size = &size
		outputs[index].SHA256 = hex.EncodeToString(hash.Sum(nil))
		if err := s.repo.SetOutputs(ctx, id, outputs); err != nil {
			return err
		}
		for suffix := 1; ; suffix++ {
			err := os.Link(stagePath, filepath.Join(s.config.DownloadDir, destination))
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("publish %s: %w", destination, err)
			}
			destination = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, filepath.Ext(name)), suffix, filepath.Ext(name))
			outputs[index].Path = destination
			if err := s.repo.SetOutputs(ctx, id, outputs); err != nil {
				return err
			}
		}
		outputs[index].State = OutputPublished
		if err := s.repo.SetOutputs(ctx, id, outputs); err != nil {
			return err
		}
		if err := os.Remove(stagePath); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sweep(ctx context.Context) error {
	s.orphanMutex.Lock()
	defer s.orphanMutex.Unlock()
	jobs, err := s.repo.ListJobs(ctx, true)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		known[job.ID] = true
	}
	for _, job := range jobs {
		if job.CleanupComplete {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if job.CleanupAfter != nil && *job.CleanupAfter > now() {
			continue
		}
		s.mutex.Lock()
		_, running := s.running[job.ID]
		s.mutex.Unlock()
		if running || job.Status == JobRunning || (job.Status == JobQueued && job.DeleteRequestedAt == nil) {
			continue
		}
		if err := s.reconcile(ctx, &job); err != nil {
			s.recordCleanupError(ctx, job.ID, err)
			continue
		}
		workspace := filepath.Join(s.config.TempDir, ".lemontube", job.ID)
		if len(job.ID) != 32 {
			s.logger.Warn("invalid workspace id", "job", job.ID)
			continue
		}
		if _, err := hex.DecodeString(job.ID); err != nil {
			continue
		}
		if err := os.RemoveAll(workspace); err != nil {
			s.recordCleanupError(ctx, job.ID, err)
			continue
		}
		if job.DeleteRequestedAt == nil && s.config.ClearAfter > 0 && job.FinishedAt != nil {
			finished, err := time.Parse(time.RFC3339Nano, *job.FinishedAt)
			if err == nil && time.Since(finished) >= s.config.ClearAfter {
				_, err = s.repo.RequestDelete(ctx, job.ID, s.config.DeleteOutputs)
				if err != nil {
					return err
				}
				job.DeleteRequestedAt = job.FinishedAt
				job.DeleteOutputs = s.config.DeleteOutputs
			}
		}
		if job.DeleteRequestedAt == nil {
			if job.CleanupError != nil {
				if err := s.repo.ClearCleanupError(ctx, job.ID); err != nil {
					return err
				}
			}
			continue
		}
		if job.DeleteOutputs {
			if err := s.deleteOutputs(job.Outputs); err != nil {
				s.recordCleanupError(ctx, job.ID, err)
				continue
			}
		}
		if !job.DeleteOutputs && len(job.Outputs) > 0 {
			if err := s.repo.CompleteCleanup(ctx, job.ID); err != nil {
				return err
			}
			continue
		}
		if err := s.repo.DeleteJob(ctx, job.ID); err != nil {
			return err
		}
	}
	return s.cleanOrphanWorkspaces(known)
}

func (s *Service) recordCleanupError(ctx context.Context, id string, cleanupErr error) {
	s.logger.Warn("cleanup failed", "job", id, "error", cleanupErr)
	if err := s.repo.RecordCleanupError(ctx, id, cleanupErr); err != nil {
		s.logger.Error("save cleanup error", "job", id, "error", err)
	}
}

// cleanOrphanWorkspaces only examines the private root and ignores recent or
// unrecognized entries. It never follows symlinks or touches download files.
func (s *Service) cleanOrphanWorkspaces(known map[string]bool) error {
	root := filepath.Join(s.config.TempDir, ".lemontube")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if known[name] || !entry.IsDir() || len(name) != 32 {
			continue
		}
		if _, err := hex.DecodeString(name); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			s.logger.Warn("cleanup orphan workspace", "workspace", name, "error", err)
		}
	}
	return nil
}

func (s *Service) reconcile(ctx context.Context, job *Job) error {
	changed := false
	for i := range job.Outputs {
		item := &job.Outputs[i]
		if filepath.Base(item.Path) != item.Path || filepath.Base(item.Stage) != item.Stage || !strings.HasPrefix(item.Stage, ".lemontube-"+job.ID+"-") {
			return errors.New("invalid output manifest")
		}
		stage := filepath.Join(s.config.DownloadDir, item.Stage)
		final := filepath.Join(s.config.DownloadDir, item.Path)
		stageInfo, err := os.Lstat(stage)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !stageInfo.Mode().IsRegular() {
			return errors.New("invalid staging file")
		}
		if item.State == OutputPublishing {
			finalInfo, err := os.Lstat(final)
			if err == nil && os.SameFile(stageInfo, finalInfo) {
				item.State = OutputPublished
				changed = true
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if changed {
			if err := s.repo.SetOutputs(ctx, job.ID, job.Outputs); err != nil {
				return err
			}
			changed = false
		}
		if err := os.Remove(stage); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteOutputs(outputs []OutputFile) error {
	for _, item := range outputs {
		if item.State != OutputPublished {
			continue
		}
		if item.Path == "." || filepath.Base(item.Path) != item.Path || item.SHA256 == "" {
			return errors.New("invalid output manifest")
		}
		path := filepath.Join(s.config.DownloadDir, item.Path)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to delete non-regular output %s", item.Path)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
			return fmt.Errorf("output %s changed; not deleting", item.Path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ClearOrphanOutputs removes unchanged outputs whose jobs have been removed.
// Files without a retained ownership record are never examined.
func (s *Service) ClearOrphanOutputs(ctx context.Context) (int, int, error) {
	if err := s.sweep(ctx); err != nil {
		return 0, 0, err
	}
	s.orphanMutex.Lock()
	defer s.orphanMutex.Unlock()
	jobs, err := s.repo.ListJobs(ctx, true)
	if err != nil {
		return 0, 0, err
	}
	cleared, retained := 0, 0
	for _, job := range jobs {
		if job.DeleteRequestedAt == nil || !job.CleanupComplete {
			continue
		}
		if err := s.deleteOutputs(job.Outputs); err != nil {
			s.logger.Warn("orphan output retained", "job", job.ID, "error", err)
			retained++
			continue
		}
		if err := s.repo.DeleteJob(ctx, job.ID); err != nil {
			return cleared, retained, err
		}
		cleared++
	}
	return cleared, retained, nil
}

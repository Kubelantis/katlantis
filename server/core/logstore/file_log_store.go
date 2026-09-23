// Copyright 2025 The Atlantis Authors
// SPDX-License-Identifier: Apache-2.0

package logstore

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/runatlantis/atlantis/server/logging"
)

// DirName is the directory under the data dir that FileLogStore writes to.
const DirName = "job-logs"

// FileLogStore persists job output as plain-text files on local disk, laid
// out as <root>/<escaped repo full name>/<pull num>/<job id>.log. Grouping by
// pull request lets DeletePull drop a closed pull request's logs in one step,
// even after a restart has emptied the output handler's memory.
type FileLogStore struct {
	root   string
	logger logging.SimpleLogging

	mu   sync.Mutex
	open map[string]*openLog
}

// openLog is the file of a job that is still producing output.
type openLog struct {
	f      *os.File
	pull   Pull
	failed bool
}

// NewFileLogStore returns a FileLogStore rooted at root, creating it if
// needed.
func NewFileLogStore(root string, logger logging.SimpleLogging) (*FileLogStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("creating job log dir %q: %w", root, err)
	}
	return &FileLogStore{root: root, logger: logger, open: map[string]*openLog{}}, nil
}

func (s *FileLogStore) Write(pull Pull, jobID, line string) {
	if !ValidJobID(jobID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	l, ok := s.open[jobID]
	if !ok {
		l = &openLog{pull: pull}
		s.open[jobID] = l
		f, err := s.create(pull, jobID)
		if err != nil {
			s.logger.Warn("job log store: persisting output for job %s: %s", jobID, err)
			l.failed = true
		}
		l.f = f
	}
	if l.failed {
		return
	}
	if _, err := l.f.WriteString(line + "\n"); err != nil {
		s.logger.Warn("job log store: persisting output for job %s: %s", jobID, err)
		l.failed = true
	}
}

func (s *FileLogStore) create(pull Pull, jobID string) (*os.File, error) {
	dir, err := s.pullDir(pull)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	// #nosec G304 -- dir is built from an escaped repo name and a pull number, and jobID is validated by ValidJobID.
	return os.OpenFile(filepath.Join(dir, jobID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func (s *FileLogStore) Complete(jobID string) {
	s.mu.Lock()
	l, ok := s.open[jobID]
	delete(s.open, jobID)
	s.mu.Unlock()
	if ok {
		s.closeLog(jobID, l)
	}
}

func (s *FileLogStore) closeLog(jobID string, l *openLog) {
	if l.f == nil {
		return
	}
	if err := errors.Join(l.f.Sync(), l.f.Close()); err != nil {
		s.logger.Warn("job log store: closing output for job %s: %s", jobID, err)
	}
}

func (s *FileLogStore) Exists(jobID string) (bool, error) {
	path, err := s.find(jobID)
	return path != "", err
}

func (s *FileLogStore) Replay(jobID string, fn func(line string) bool) error {
	path, err := s.find(jobID)
	if err != nil || path == "" {
		return err
	}
	// #nosec G304 -- path was found by listing root for a validated job ID.
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // deleted with its pull request in the meantime
		}
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if line != "" && !fn(strings.TrimSuffix(line, "\n")) {
			return nil
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *FileLogStore) DeletePull(pull Pull) error {
	dir, err := s.pullDir(pull)
	if err != nil {
		return err
	}

	s.mu.Lock()
	for jobID, l := range s.open {
		if l.pull == pull {
			delete(s.open, jobID)
			s.closeLog(jobID, l)
		}
	}
	s.mu.Unlock()

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("deleting job logs for %s#%d: %w", pull.RepoFullName, pull.Num, err)
	}
	// Drop the repo directory too once its last pull request is gone.
	_ = os.Remove(filepath.Dir(dir))
	return nil
}

func (s *FileLogStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for jobID, l := range s.open {
		s.closeLog(jobID, l)
	}
	s.open = map[string]*openLog{}
	return nil
}

// pullDir is where pull's job logs live. The repo full name is path-escaped
// into a single segment, so "owner/repo" and GitLab subgroups alike map to one
// directory and can never climb out of root.
func (s *FileLogStore) pullDir(pull Pull) (string, error) {
	repo := url.PathEscape(pull.RepoFullName)
	if repo == "" || repo == "." || repo == ".." {
		return "", fmt.Errorf("invalid repo name %q", pull.RepoFullName)
	}
	return filepath.Join(s.root, repo, strconv.Itoa(pull.Num)), nil
}

// find returns the path of jobID's log, or "" if there is none. Job IDs are
// unique across pull requests, so the first match is the only one.
func (s *FileLogStore) find(jobID string) (string, error) {
	if !ValidJobID(jobID) {
		return "", nil
	}
	repos, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	name := jobID + ".log"
	for _, repo := range repos {
		if !repo.IsDir() {
			continue
		}
		pulls, err := os.ReadDir(filepath.Join(s.root, repo.Name()))
		if err != nil {
			continue // removed by a concurrent DeletePull
		}
		for _, pull := range pulls {
			path := filepath.Join(s.root, repo.Name(), pull.Name(), name)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	return "", nil
}

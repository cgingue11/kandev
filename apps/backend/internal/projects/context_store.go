package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/kandev/kandev/internal/worktree"
)

var (
	ErrInvalidContextPath = errors.New("invalid project context path")
	ErrContextConflict    = errors.New("project context changed since it was read")
	ErrContextNotFound    = errors.New("project context file not found")
)

const maxContextFileBytes = 5 << 20

// ContextStore owns the durable filesystem area shared by project tasks.
type ContextStore struct {
	root string
	mu   sync.Mutex
}

type ContextEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int64  `json:"size,omitempty"`
}

func NewContextStore(root string) *ContextStore {
	return &ContextStore{root: filepath.Clean(root)}
}

func (s *ContextStore) ContextPath(projectID string) (string, error) {
	id, err := canonicalProjectID(projectID)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", fmt.Errorf("resolve project context root: %w", err)
	}
	return filepath.Join(root, id, "context"), nil
}

func (s *ContextStore) Provision(_ context.Context, projectID string) (string, error) {
	root, err := s.ContextPath(projectID)
	if err != nil {
		return "", err
	}
	if err := mkdirNoFollow(root); err != nil {
		return "", fmt.Errorf("create project context: %w", err)
	}
	notes := filepath.Join(root, "notes.md")
	info, err := os.Lstat(notes)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: notes.md is not a regular file", ErrInvalidContextPath)
		}
		return root, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect initial project notes: %w", err)
	}
	file, err := os.OpenFile(notes, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return root, nil
		}
		return "", fmt.Errorf("create initial project notes: %w", err)
	}
	if _, err := file.WriteString("# Project notes\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(notes)
		return "", fmt.Errorf("write initial project notes: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close initial project notes: %w", err)
	}
	return root, nil
}

func (s *ContextStore) EnsureTaskLink(taskRoot, projectID, taskID, taskDirName string) error {
	target, err := s.ContextPath(projectID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("project context is unavailable: %w", err)
	}
	_, err = worktree.EnsureOwnedDirectoryLink(taskRoot, "context", target,
		worktree.OwnedDirectoryLinkOwner{TaskID: taskID, TaskDirName: taskDirName})
	return err
}

func RemoveTaskContextLink(taskRoot string) error {
	return worktree.RemoveOwnedDirectoryLink(taskRoot, "context")
}

func (s *ContextStore) RemoveProject(projectID string) error {
	path, err := s.ContextPath(projectID)
	if err != nil {
		return err
	}
	if err := rejectSymlinkAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect project context for removal: %w", err)
	}
	if !info.IsDir() || worktree.IsDirectoryLink(path) {
		return fmt.Errorf("%w: project context root is not a real directory", ErrInvalidContextPath)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove project context: %w", err)
	}
	return nil
}

func (s *ContextStore) List(projectID, relative string) ([]ContextEntry, error) {
	directory, clean, err := s.resolvePath(projectID, relative, true)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrContextNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("list project context: %w", err)
	}
	result := make([]ContextEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		entryPath := entry.Name()
		if clean != "" {
			entryPath = filepath.ToSlash(filepath.Join(clean, entryPath))
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		result = append(result, ContextEntry{Name: entry.Name(), Path: entryPath, Kind: kind, Size: info.Size()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *ContextStore) ReadFile(projectID, relative string) (string, string, error) {
	path, _, err := s.resolvePath(projectID, relative, false)
	if err != nil {
		return "", "", err
	}
	data, err := readRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", ErrContextNotFound
	}
	if err != nil {
		return "", "", err
	}
	return string(data), contentHash(data), nil
}

func (s *ContextStore) WriteFile(projectID, relative, expectedHash string, content []byte) (string, error) {
	if len(content) > maxContextFileBytes {
		return "", fmt.Errorf("%w: file exceeds %d bytes", ErrInvalidContextPath, maxContextFileBytes)
	}
	path, _, err := s.resolvePath(projectID, relative, false)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkContextWriteVersion(path, expectedHash); err != nil {
		return "", err
	}
	return writeContextFileAtomically(path, content)
}

func checkContextWriteVersion(path, expectedHash string) error {
	current, readErr := readRegularFile(path)
	switch {
	case errors.Is(readErr, os.ErrNotExist):
		if expectedHash != "" {
			return ErrContextConflict
		}
	case readErr != nil:
		return readErr
	case expectedHash == "" || contentHash(current) != expectedHash:
		return ErrContextConflict
	}
	return nil
}

func writeContextFileAtomically(path string, content []byte) (string, error) {
	if err := rejectSymlinkAncestors(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create project context directory: %w", err)
	}
	if err := rejectSymlinkAncestors(filepath.Dir(path)); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".context-write-*")
	if err != nil {
		return "", fmt.Errorf("create project context temporary file: %w", err)
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write project context: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("sync project context: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close project context: %w", err)
	}
	if err := recheckRegularOrMissing(path); err != nil {
		return "", err
	}
	if err := os.Rename(temp, path); err != nil {
		return "", fmt.Errorf("commit project context write: %w", err)
	}
	return contentHash(content), nil
}

func (s *ContextStore) resolvePath(projectID, relative string, directory bool) (string, string, error) {
	root, err := s.ContextPath(projectID)
	if err != nil {
		return "", "", err
	}
	clean, err := cleanContextRelativePath(relative, directory)
	if err != nil {
		return "", "", err
	}
	path := root
	if clean != "" {
		path = filepath.Join(root, filepath.FromSlash(clean))
	}
	if err := rejectContainedPathSymlinks(root, path, directory); err != nil {
		return "", "", err
	}
	return path, clean, nil
}

func cleanContextRelativePath(relative string, allowRoot bool) (string, error) {
	if relative == "" || relative == "." {
		if allowRoot {
			return "", nil
		}
		return "", ErrInvalidContextPath
	}
	if strings.ContainsRune(relative, '\x00') || filepath.IsAbs(relative) || strings.Contains(relative, `\`) {
		return "", ErrInvalidContextPath
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return "", ErrInvalidContextPath
	}
	return clean, nil
}

func rejectContainedPathSymlinks(root, path string, allowMissingLeaf bool) error {
	rel, err := containedRelativePath(root, path)
	if err != nil {
		return err
	}
	if err := rejectSymlinkAncestors(root); err != nil {
		return err
	}
	current := root
	if rel == "." {
		return nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		if err := validateContainedPathComponent(current, index, len(parts), allowMissingLeaf); err != nil {
			return err
		}
	}
	return nil
}

func containedRelativePath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrInvalidContextPath
	}
	return rel, nil
}

func validateContainedPathComponent(path string, index, length int, allowMissingLeaf bool) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if allowMissingLeaf || index < length-1 {
			return nil
		}
		return nil
	case err != nil:
		return fmt.Errorf("inspect project context path: %w", err)
	case info.Mode()&os.ModeSymlink != 0 || worktree.IsDirectoryLink(path) || index < length-1 && !info.IsDir():
		return ErrInvalidContextPath
	default:
		return nil
	}
}

func rejectSymlinkAncestors(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	rel := strings.TrimPrefix(absolute, current)
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || worktree.IsDirectoryLink(current) {
			return ErrInvalidContextPath
		}
	}
	return nil
}

func mkdirNoFollow(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	rel := strings.TrimPrefix(absolute, current)
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || worktree.IsDirectoryLink(current) {
			return ErrInvalidContextPath
		}
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrInvalidContextPath
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxContextFileBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maxContextFileBytes {
		return nil, fmt.Errorf("%w: file exceeds %d bytes", ErrInvalidContextPath, maxContextFileBytes)
	}
	return data, nil
}

func recheckRegularOrMissing(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrInvalidContextPath
	}
	return nil
}

func contentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func canonicalProjectID(projectID string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil || id.String() != strings.ToLower(strings.TrimSpace(projectID)) {
		return "", ErrInvalidContextPath
	}
	return id.String(), nil
}

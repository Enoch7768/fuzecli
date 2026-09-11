package workspace

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CreateSnapshot stores a point-in-time copy of selected workspace files in
// .aicli/snapshots. It is intentionally explicit so a caller can snapshot
// before a risky operation without changing normal generation semantics.
func (s *Store) CreateSnapshot(paths []string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("workspace store is nil")
	}
	policy, err := LoadIgnorePolicy(s.Root)
	if err != nil {
		return "", err
	}

	unique := make(map[string]struct{}, len(paths))
	for _, rel := range paths {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || policy.Ignored(rel) {
			continue
		}
		unique[rel] = struct{}{}
	}

	if len(unique) == 0 {
		return "", fmt.Errorf("snapshot contains no eligible files")
	}

	names := make([]string, 0, len(unique))
	for rel := range unique {
		names = append(names, rel)
	}
	sort.Strings(names)

	dir := filepath.Join(s.Root, ".aicli", "snapshots")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}

	name := time.Now().UTC().Format("20060102-150405.000000000") + ".zip"
	target := filepath.Join(dir, name)
	tmp := target + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return "", err
	}

	zw := zip.NewWriter(file)
	for _, rel := range names {
		path := filepath.Join(s.Root, filepath.FromSlash(rel))
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", fmt.Errorf("snapshot %s: %w", rel, readErr)
		}
		entry, createErr := zw.Create(rel)
		if createErr != nil {
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", createErr
		}
		if _, writeErr := entry.Write(data); writeErr != nil {
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", writeErr
		}
	}

	if err := zw.Close(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

func (s *Store) LatestSnapshot() (string, error) {
	dir := filepath.Join(s.Root, ".aicli", "snapshots")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no snapshots exist")
	}
	if err != nil {
		return "", err
	}
	var latest string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		if latest == "" || entry.Name() > filepath.Base(latest) {
			latest = filepath.Join(dir, entry.Name())
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no snapshots exist")
	}
	return latest, nil
}

// RestoreSnapshot restores the files contained in a snapshot. It never
// deletes files that are not present in the snapshot.
func (s *Store) RestoreSnapshot(snapshot string) ([]string, error) {
	snapshot, err := filepath.Abs(snapshot)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(snapshot)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	zr, err := zip.NewReader(file, fileSize(file))
	if err != nil {
		return nil, err
	}

	policy, err := LoadIgnorePolicy(root)
	if err != nil {
		return nil, err
	}
	var restored []string
	for _, entry := range zr.File {
		rel := filepath.ToSlash(filepath.Clean(entry.Name))
		if rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || policy.Ignored(rel) {
			continue
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return restored, err
		}
		reader, err := entry.Open()
		if err != nil {
			return restored, err
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return restored, readErr
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return restored, err
		}
		restored = append(restored, rel)
	}
	return restored, nil
}

func fileSize(file *os.File) int64 {
	info, err := file.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}

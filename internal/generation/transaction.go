package generation

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type snapshotManifest struct {
	Version int               `json:"version"`
	Created time.Time         `json:"created_at"`
	Files   []snapshotFileRef `json:"files"`
}

type snapshotFileRef struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// createAutomaticSnapshot captures every path touched by a plan before any
// filesystem mutation. It also records files that do not yet exist, allowing a
// future restore to remove files created by the plan.
func createAutomaticSnapshot(root string, plan Plan) (string, error) {
	unique := make(map[string]struct{}, len(plan.Files))
	for _, change := range plan.Files {
		rel := filepath.ToSlash(filepath.Clean(change.Path))
		if rel == "." || filepath.IsAbs(change.Path) || filepath.VolumeName(change.Path) != "" || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return "", fmt.Errorf("unsafe snapshot path: %q", change.Path)
		}
		unique[rel] = struct{}{}
	}
	if len(unique) == 0 {
		return "", fmt.Errorf("cannot snapshot an empty plan")
	}

	paths := make([]string, 0, len(unique))
	for rel := range unique {
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	dir := filepath.Join(root, ".aicli", "snapshots")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	target := filepath.Join(dir, stamp+"-auto.zip")
	tmp := target + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create snapshot: %w", err)
	}

	manifest := snapshotManifest{Version: 1, Created: time.Now().UTC()}
	for _, rel := range paths {
		targetPath, err := Resolve(root, rel)
		if err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", err
		}
		_, statErr := os.Lstat(targetPath)
		exists := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", fmt.Errorf("inspect %s before snapshot: %w", rel, statErr)
		}
		manifest.Files = append(manifest.Files, snapshotFileRef{Path: rel, Exists: exists})
	}

	zw := zip.NewWriter(file)
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		_ = zw.Close()
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	entry, err := zw.Create(".aicli-manifest.json")
	if err != nil {
		_ = zw.Close()
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	if _, err := entry.Write(manifestData); err != nil {
		_ = zw.Close()
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", err
	}

	for _, ref := range manifest.Files {
		if !ref.Exists {
			continue
		}
		path, err := Resolve(root, ref.Path)
		if err != nil {
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", fmt.Errorf("read %s for snapshot: %w", ref.Path, err)
		}
		entry, err := zw.Create(ref.Path)
		if err != nil {
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", err
		}
		if _, err := entry.Write(data); err != nil {
			_ = zw.Close()
			_ = file.Close()
			_ = os.Remove(tmp)
			return "", err
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


// CreatePlanSnapshot captures every path a plan may change, including paths that do not exist yet.
func CreatePlanSnapshot(root string, plan Plan) (string, error) {
	return createAutomaticSnapshot(root, plan)
}

// RestorePlanSnapshot restores original file contents and removes files created by the failed plan.
func RestorePlanSnapshot(root, snapshot string) error {
	archive, err := zip.OpenReader(snapshot)
	if err != nil {
		return fmt.Errorf("open rollback snapshot: %w", err)
	}
	defer archive.Close()

	var manifest snapshotManifest
	for _, entry := range archive.File {
		if entry.Name != ".aicli-manifest.json" {
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return readErr
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("read rollback manifest: %w", err)
		}
		break
	}
	if manifest.Version != 1 || len(manifest.Files) == 0 {
		return fmt.Errorf("rollback snapshot has an invalid manifest")
	}

	entries := make(map[string]*zip.File, len(archive.File))
	for _, entry := range archive.File {
		entries[filepath.ToSlash(entry.Name)] = entry
	}
	for _, ref := range manifest.Files {
		target, err := Resolve(root, ref.Path)
		if err != nil {
			return err
		}
		if !ref.Exists {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove newly created %s during rollback: %w", ref.Path, err)
			}
			continue
		}
		entry, ok := entries[filepath.ToSlash(ref.Path)]
		if !ok {
			return fmt.Errorf("rollback snapshot is missing %s", ref.Path)
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return readErr
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		tmp := target + ".fuzerollback"
		if err := os.WriteFile(tmp, data, 0644); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return nil
}

package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

func memoryPath(root string) string {
	return filepath.Join(root, ".aicli", "memory.json")
}

func LoadMemory(root string) (ProjectMemory, error) {
	if strings.TrimSpace(root) == "" {
		return ProjectMemory{}, errors.New("workspace root is required")
	}
	path := memoryPath(root)
	if err := rejectSymlinkComponents(root, path); err != nil {
		return ProjectMemory{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ProjectMemory{}, nil
	}
	if err != nil {
		return ProjectMemory{}, err
	}
	if !info.Mode().IsRegular() {
		return ProjectMemory{}, errors.New("project memory must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectMemory{}, err
	}
	var memory ProjectMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return ProjectMemory{}, fmt.Errorf("decode project memory: %w", err)
	}
	return memory, nil
}

func SaveMemory(root string, memory ProjectMemory) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("workspace root is required")
	}
	path := memoryPath(root)
	if err := rejectSymlinkComponents(root, path); err != nil {
		return err
	}
	memory.UpdatedAt = time.Now().UTC()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(root, path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(memory, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + "." + uuid.NewString() + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func UpdateMemory(root string, update func(*ProjectMemory)) error {
	memory, err := LoadMemory(root)
	if err != nil {
		return err
	}
	update(&memory)
	return SaveMemory(root, memory)
}

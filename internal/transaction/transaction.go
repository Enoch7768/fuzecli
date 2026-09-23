package transaction

import (
	"fmt"
	"os"
	"path/filepath"
)

type Snapshot struct {
	Path    string
	Exists  bool
	Content []byte
	Mode    os.FileMode
}

type Transaction struct {
	snapshots map[string]Snapshot
	changed   []string
	closed    bool
}

func New() *Transaction {
	return &Transaction{snapshots: make(map[string]Snapshot)}
}

func (t *Transaction) Capture(path string) error {
	if t.closed {
		return fmt.Errorf("transaction is closed")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, ok := t.snapshots[path]; ok {
		return nil
	}

	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("cannot capture directory: %s", path)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("capture %s: %w", path, readErr)
		}
		t.snapshots[path] = Snapshot{
			Path:    path,
			Exists:  true,
			Content: content,
			Mode:    info.Mode(),
		}
		return nil
	}
	if os.IsNotExist(err) {
		t.snapshots[path] = Snapshot{Path: path}
		return nil
	}
	return fmt.Errorf("inspect %s: %w", path, err)
}

func (t *Transaction) Write(path string, content []byte, mode os.FileMode) error {
	if err := t.Capture(path); err != nil {
		return err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	temp := fmt.Sprintf("%s.fuzetx", path)
	if err := os.WriteFile(temp, content, mode.Perm()); err != nil {
		return fmt.Errorf("stage %s: %w", path, err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("commit %s: %w", path, err)
	}
	t.changed = append(t.changed, path)
	return nil
}

func (t *Transaction) Delete(path string) error {
	if err := t.Capture(path); err != nil {
		return err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	t.changed = append(t.changed, path)
	return nil
}

func (t *Transaction) Rollback() error {
	if t.closed {
		return fmt.Errorf("transaction is closed")
	}
	var firstErr error
	for i := len(t.changed) - 1; i >= 0; i-- {
		path := t.changed[i]
		snapshot, ok := t.snapshots[path]
		if !ok {
			continue
		}
		if snapshot.Exists {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			temp := fmt.Sprintf("%s.fuzerollback", path)
			if err := os.WriteFile(temp, snapshot.Content, snapshot.Mode.Perm()); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := os.Rename(temp, path); err != nil {
				_ = os.Remove(temp)
				if firstErr == nil {
					firstErr = err
				}
			}
		} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	t.closed = true
	return firstErr
}

func (t *Transaction) Commit() {
	t.closed = true
	t.snapshots = nil
	t.changed = nil
}

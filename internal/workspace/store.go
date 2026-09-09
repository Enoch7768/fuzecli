package workspace

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/provider"
	_ "modernc.org/sqlite"
)

type State struct {
	FileHashes     map[string]string `json:"file_hashes"`
	ActiveProvider string            `json:"active_provider"`
	ActiveModel    string            `json:"active_model"`
}
type Store struct {
	Root string
	DB   *sql.DB
}

func Init(root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, ".aicli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create .aicli: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "session.db"))
	if err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	schema := `CREATE TABLE IF NOT EXISTS messages(id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL);CREATE TABLE IF NOT EXISTS sessions(id INTEGER PRIMARY KEY AUTOINCREMENT, started_at TEXT NOT NULL, ended_at TEXT);CREATE TABLE IF NOT EXISTS touched_files(path TEXT PRIMARY KEY);`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize session database: %w", err)
	}
	return &Store{Root: root, DB: db}, nil
}
func Open(root string) (*Store, error) { return Init(root) }
func (s *Store) Close() error          { return s.DB.Close() }
func (s *Store) AddMessage(m provider.Message) error {
	_, err := s.DB.Exec(`INSERT INTO messages(role,content,created_at) VALUES(?,?,?)`, m.Role, m.Content, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save conversation message: %w", err)
	}
	return nil
}
func (s *Store) History(limit int) ([]provider.Message, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.DB.Query(`SELECT role,content FROM messages ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rev []provider.Message
	for rows.Next() {
		var m provider.Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		rev = append(rev, m)
	}
	out := make([]provider.Message, len(rev))
	for i := range rev {
		out[len(rev)-1-i] = rev[i]
	}
	return out, rows.Err()
}
func (s *Store) MarkTouched(paths []string) error {
	for _, p := range paths {
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO touched_files(path) VALUES(?)`, p); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Touched() ([]string, error) {
	rows, err := s.DB.Query(`SELECT path FROM touched_files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) LoadState() (State, error) {
	p := filepath.Join(s.Root, ".aicli", "state.json")
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return State{FileHashes: map[string]string{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	if st.FileHashes == nil {
		st.FileHashes = map[string]string{}
	}
	return st, nil
}
func (s *Store) SaveState(st State) error {
	p := filepath.Join(s.Root, ".aicli", "state.json")
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
func (s *Store) RefreshHashes(paths []string) error {
	st, err := s.LoadState()
	if err != nil {
		return err
	}
	for _, rel := range paths {
		path := filepath.Join(s.Root, filepath.FromSlash(rel))
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		h := sha256.Sum256(b)
		st.FileHashes[rel] = hex.EncodeToString(h[:])
	}
	return s.SaveState(st)
}
func (s *Store) WorkspaceContext() (string, error) {
	paths, err := s.Touched()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, rel := range paths {
		path, err := filepath.Abs(filepath.Join(s.Root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", rel, string(data))
	}
	return b.String(), nil
}

package intelligence

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Symbol struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
	Line int    `json:"line"`
}

type Match struct {
	Path    string   `json:"path"`
	Line    int      `json:"line"`
	Text    string   `json:"text"`
	Symbols []Symbol `json:"symbols,omitempty"`
}

type Index struct {
	Root    string
	Files   []string
	Symbols []Symbol
}

var (
	goFunc   = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
	goType   = regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(struct|interface|[A-Za-z_][A-Za-z0-9_\[\]]*)`)
	pyDef    = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)`)
	jsDef    = regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	classDef = regexp.MustCompile(`^\s*(?:export\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
)

func Build(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{Root: root}
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path != root && ignoredDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if ignoredPath(rel) {
			return nil
		}
		idx.Files = append(idx.Files, filepath.ToSlash(rel))
		symbols, err := symbolsInFile(root, path)
		if err != nil {
			return nil
		}
		idx.Symbols = append(idx.Symbols, symbols...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(idx.Files)
	sort.Slice(idx.Symbols, func(i, j int) bool {
		if idx.Symbols[i].Path == idx.Symbols[j].Path {
			return idx.Symbols[i].Line < idx.Symbols[j].Line
		}
		return idx.Symbols[i].Path < idx.Symbols[j].Path
	})
	return idx, nil
}

func (i *Index) Search(query string, limit int) ([]Match, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}
	if limit <= 0 {
		limit = 20
	}
	terms := strings.Fields(strings.ToLower(query))
	var out []Match
	for _, rel := range i.Files {
		path := filepath.Join(i.Root, filepath.FromSlash(rel))
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 4096), 1024*1024)
		lineNo := 0
		for s.Scan() {
			lineNo++
			text := s.Text()
			lower := strings.ToLower(text)
			matched := true
			for _, term := range terms {
				if !containsSearchTerm(lower, term) {
					matched = false
					break
				}
			}
			if matched {
				out = append(out, Match{Path: rel, Line: lineNo, Text: strings.TrimSpace(text)})
				if len(out) >= limit {
					_ = f.Close()
					return out, nil
				}
			}
		}
		_ = f.Close()
		if err := s.Err(); err != nil {
			continue
		}
	}
	return out, nil
}

// containsSearchTerm requires a complete token match. This prevents a query such
// as "hello world" from matching the single identifier "HelloWorld", while still
// allowing punctuation-separated text such as "hello, world" to match.
func containsSearchTerm(line, term string) bool {
	if term == "" {
		return true
	}
	pattern := `(?i)(^|[^[:alnum:]_$])` + regexp.QuoteMeta(term) + `([^[:alnum:]_$]|$)`
	return regexp.MustCompile(pattern).MatchString(line)
}

func (i *Index) FindSymbols(query string, limit int) []Symbol {
	q := strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = 50
	}
	out := make([]Symbol, 0, limit)
	for _, s := range i.Symbols {
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(strings.ToLower(s.Kind), q) || strings.Contains(strings.ToLower(s.Path), q) {
			out = append(out, s)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func symbolsInFile(root, path string) ([]Symbol, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1024*1024)
	var out []Symbol
	line := 0
	for s.Scan() {
		line++
		text := s.Text()
		if m := goFunc.FindStringSubmatch(text); len(m) == 2 {
			out = append(out, Symbol{m[1], "function", filepath.ToSlash(rel), line})
			continue
		}
		if m := goType.FindStringSubmatch(text); len(m) == 3 {
			out = append(out, Symbol{m[1], "type", filepath.ToSlash(rel), line})
			continue
		}
		if m := pyDef.FindStringSubmatch(text); len(m) == 2 {
			out = append(out, Symbol{m[1], "function", filepath.ToSlash(rel), line})
			continue
		}
		if m := jsDef.FindStringSubmatch(text); len(m) == 2 {
			out = append(out, Symbol{m[1], "function", filepath.ToSlash(rel), line})
			continue
		}
		if m := classDef.FindStringSubmatch(text); len(m) == 2 {
			out = append(out, Symbol{m[1], "class", filepath.ToSlash(rel), line})
		}
	}
	return out, s.Err()
}

func ignoredDir(name string) bool {
	switch name {
	case ".git", ".aicli", "node_modules", "vendor", "dist", "build", "target", ".venv", "venv":
		return true
	}
	return false
}

func ignoredPath(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	if strings.HasPrefix(lower, ".env") || strings.Contains(lower, "credential") || strings.Contains(lower, "secret") || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	return false
}

func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".php", ".java", ".kt", ".rs", ".c", ".h", ".cpp", ".hpp", ".cs", ".rb", ".swift", ".sql", ".vue", ".svelte", ".html", ".css", ".scss", ".json", ".yaml", ".yml", ".toml":
		return true
	}
	return false
}

package repository

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Enoch7768/fuzecli/internal/workspace"
)

type File struct {
	Path     string   `json:"path"`
	Language string   `json:"language"`
	Bytes    int64    `json:"bytes"`
	Lines    int      `json:"lines"`
	Imports  []string `json:"imports,omitempty"`
	Symbols  []string `json:"symbols,omitempty"`
}

type Index struct {
	Root  string `json:"root"`
	Files []File `json:"files"`
}

func Build(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	policy, err := workspace.LoadIgnorePolicy(root)
	if err != nil {
		return nil, fmt.Errorf("load ignore policy: %w", err)
	}

	index := &Index{Root: root}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if policy.Ignored(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if skipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isIndexablePath(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		file := File{
			Path:     rel,
			Language: languageForPath(rel),
			Bytes:    info.Size(),
		}
		file.Lines, file.Imports, file.Symbols = analyzeFile(path, file.Language)
		index.Files = append(index.Files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(index.Files, func(i, j int) bool {
		return index.Files[i].Path < index.Files[j].Path
	})
	return index, nil
}

func (i *Index) Refresh() error {
	next, err := Build(i.Root)
	if err != nil {
		return err
	}
	i.Files = next.Files
	return nil
}

func (i *Index) Summary() string {
	if i == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Repository index: %d files\n", len(i.Files)))
	languages := map[string]int{}
	for _, file := range i.Files {
		languages[file.Language]++
	}
	langs := make([]string, 0, len(languages))
	for language := range languages {
		langs = append(langs, language)
	}
	sort.Strings(langs)
	if len(langs) > 0 {
		b.WriteString("Languages: ")
		for n, language := range langs {
			if n > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("%s=%d", language, languages[language]))
		}
		b.WriteByte('\n')
	}
	for _, file := range i.Files {
		b.WriteString(fmt.Sprintf("- %s [%s, %d lines]", file.Path, file.Language, file.Lines))
		if len(file.Symbols) > 0 {
			b.WriteString(" symbols=" + strings.Join(limitStrings(file.Symbols, 12), ","))
		}
		if len(file.Imports) > 0 {
			b.WriteString(" imports=" + strings.Join(limitStrings(file.Imports, 8), ","))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (i *Index) Context(query string, limit int) string {
	files := i.Relevant(query, limit)
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Repository intelligence matches:\n")
	for _, file := range files {
		b.WriteString("- " + file.Path + " [" + file.Language + ", " + fmt.Sprintf("%d", file.Lines) + " lines]")
		if len(file.Symbols) > 0 {
			b.WriteString(" symbols=" + strings.Join(limitStrings(file.Symbols, 16), ","))
		}
		if len(file.Imports) > 0 {
			b.WriteString(" imports=" + strings.Join(limitStrings(file.Imports, 10), ","))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (i *Index) Relevant(query string, limit int) []File {
	if i == nil || limit <= 0 {
		return nil
	}
	terms := tokenize(query)
	type scored struct {
		file  File
		score int
	}
	items := make([]scored, 0, len(i.Files))
	for _, file := range i.Files {
		score := scoreFile(file, terms)
		if score > 0 {
			items = append(items, scored{file: file, score: score})
		}
	}
	sort.Slice(items, func(a, b int) bool {
		if items[a].score != items[b].score {
			return items[a].score > items[b].score
		}
		return items[a].file.Path < items[b].file.Path
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]File, len(items))
	for n := range items {
		out[n] = items[n].file
	}
	return out
}

func scoreFile(file File, terms []string) int {
	text := strings.ToLower(strings.Join(append([]string{file.Path, file.Language}, append(file.Symbols, file.Imports...)...), " "))
	score := 0
	for _, term := range terms {
		if strings.Contains(strings.ToLower(file.Path), term) {
			score += 8
		}
		if strings.Contains(text, term) {
			score += 3
		}
		for _, symbol := range file.Symbols {
			if strings.EqualFold(symbol, term) {
				score += 12
			}
		}
	}
	return score
}

func analyzeFile(path, language string) (int, []string, []string) {
	file, err := os.Open(path)
	if err != nil {
		return 0, nil, nil
	}
	defer file.Close()

	if language == "go" {
		if parsed, err := parser.ParseFile(token.NewFileSet(), path, file, parser.ImportsOnly|parser.ParseComments); err == nil {
			imports := make([]string, 0, len(parsed.Imports))
			for _, spec := range parsed.Imports {
				if spec.Path != nil {
					imports = append(imports, strings.Trim(spec.Path.Value, """))
				}
			}
			data, _ := os.ReadFile(path)
			lines := countLines(data)
			if full, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ParseComments); err == nil {
				symbols := goSymbols(full)
				return lines, imports, symbols
			}
			return lines, imports, nil
		}
	}

	scanner := bufio.NewScanner(file)
	lines := 0
	var imports []string
	for scanner.Scan() {
		lines++
		line := strings.TrimSpace(scanner.Text())
		if len(imports) < 24 {
			if value := genericImport(line, language); value != "" {
				imports = append(imports, value)
			}
		}
	}
	return lines, unique(imports), nil
}

func goSymbols(file *ast.File) []string {
	var symbols []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil {
				symbols = append(symbols, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					symbols = append(symbols, s.Name.Name)
				case *ast.ValueSpec:
					for _, name := range s.Names {
						symbols = append(symbols, name.Name)
					}
				}
			}
		}
	}
	return unique(symbols)
}

func genericImport(line, language string) string {
	switch language {
	case "javascript", "typescript":
		if strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "export ") {
			return line
		}
		if strings.HasPrefix(line, "const ") && strings.Contains(line, "require(") {
			return line
		}
	case "python":
		if strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ") {
			return line
		}
	case "php":
		if strings.HasPrefix(line, "use ") || strings.HasPrefix(line, "require") || strings.HasPrefix(line, "include") {
			return line
		}
	}
	return ""
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".py":
		return "python"
	case ".php":
		return "php"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".swift":
		return "swift"
	case ".sql":
		return "sql"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	case ".sh", ".bash":
		return "shell"
	default:
		return "text"
	}
}

func isIndexablePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".bmp", ".tif", ".tiff", ".svgz", ".pdf", ".zip", ".7z", ".rar", ".gz", ".tar", ".exe", ".dll", ".so", ".dylib", ".bin", ".mp3", ".wav", ".flac", ".mp4", ".mov", ".avi", ".mkv", ".woff", ".woff2", ".ttf", ".otf":
		return false
	default:
		return true
	}
}

func skipDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".aicli", ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "target", ".next", ".nuxt", ".cache", "coverage", ".idea", ".vscode":
		return true
	default:
		return false
	}
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	return strings.Count(string(data), "\n") + 1
}

func tokenize(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) >= 2 {
			out = append(out, field)
		}
	}
	return unique(out)
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

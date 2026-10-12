package platform

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Enoch7768/fuzecli/internal/repository"
)

func BuildGraph(root string) (ContextGraph, error) {
	index, err := repository.Build(root)
	if err != nil {
		return ContextGraph{}, err
	}
	graph := ContextGraph{}
	for _, file := range index.Files {
		graph.Nodes = append(graph.Nodes, GraphNode{
			Path: file.Path, Language: file.Language,
			Symbols: append([]string(nil), file.Symbols...),
			Imports: append([]string(nil), file.Imports...),
		})
		for _, imp := range file.Imports {
			if target := resolveImport(root, file.Path, imp); target != "" {
				graph.Edges = append(graph.Edges, GraphEdge{From: file.Path, To: target, Kind: "imports"})
			}
		}
	}
	return graph, nil
}

func resolveImport(root, from, imp string) string {
	imp = strings.TrimSpace(imp)
	if imp == "" {
		return ""
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err == nil {
		rootAbs = rootReal
	}
	base := filepath.Dir(filepath.Join(rootAbs, filepath.FromSlash(from)))
	candidates := []string{
		imp, imp + ".go", imp + ".ts", imp + ".tsx", imp + ".js",
		imp + ".jsx", imp + ".py", filepath.Join(imp, "index.ts"),
		filepath.Join(imp, "index.js"),
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) {
			continue
		}
		path := filepath.Clean(filepath.Join(base, filepath.FromSlash(candidate)))
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		resolvedRel, err := filepath.Rel(rootAbs, resolved)
		if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
			continue
		}
		info, err := os.Stat(resolved)
		if err == nil && !info.IsDir() {
			return filepath.ToSlash(resolvedRel)
		}
	}
	return ""
}

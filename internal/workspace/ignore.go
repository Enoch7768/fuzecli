package workspace

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IgnorePolicy controls which workspace paths are allowed into model context.
// Hard-sensitive paths can never be re-enabled by .aicliignore.
type IgnorePolicy struct {
	root        string
	patterns    []string
	hardBlocked []string
}

var hardBlockedPatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.jks",
	"*.kdbx",
	"id_rsa",
	"id_rsa.*",
	"id_ed25519",
	"id_ed25519.*",
	".npmrc",
	".pypirc",
	".netrc",
	"credentials.json",
	"credentials.yaml",
	"credentials.yml",
	"secrets.json",
	"secrets.yaml",
	"secrets.yml",
}

func LoadIgnorePolicy(root string) (IgnorePolicy, error) {
	policy := IgnorePolicy{
		root:        root,
		hardBlocked: append([]string(nil), hardBlockedPatterns...),
	}

	file, err := os.Open(filepath.Join(root, ".aicliignore"))
	if os.IsNotExist(err) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		policy.patterns = append(policy.patterns, filepath.ToSlash(line))
	}
	if err := scanner.Err(); err != nil {
		return policy, err
	}
	return policy, nil
}

// Ignored reports whether rel must not be included in AI workspace context.
func (p IgnorePolicy) Ignored(rel string) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" || rel == ".aicliignore" {
		return false
	}

	base := path.Base(rel)
	for _, pattern := range p.hardBlocked {
		if ignoreMatch(pattern, rel, base) {
			return true
		}
	}
	for _, pattern := range p.patterns {
		if ignoreMatch(pattern, rel, base) {
			return true
		}
	}
	return false
}

func ignoreMatch(pattern, rel, base string) bool {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == "" {
		return false
	}

	if strings.HasSuffix(pattern, "/") {
		pattern = strings.TrimSuffix(pattern, "/")
		return rel == pattern || strings.HasPrefix(rel, pattern+"/")
	}

	if strings.Contains(pattern, "/") {
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		return strings.HasPrefix(rel, pattern+"/")
	}

	if ok, _ := path.Match(pattern, base); ok {
		return true
	}
	return strings.HasPrefix(rel, pattern+"/")
}

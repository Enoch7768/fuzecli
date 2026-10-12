package platform

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func LoadRules(root string) ([]Rule, error) {
	dir := filepath.Join(root, ".aicli", "rules")
	if err := rejectSymlinkComponents(root, dir); err != nil {
		return nil, err
	}
	return loadMarkdownDirectory(root, dir)
}

func LoadSkills(root string) ([]Skill, error) {
	dir := filepath.Join(root, ".aicli", "skills")
	if err := rejectSymlinkComponents(root, dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var skills []Skill
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if err := rejectSymlinkComponents(root, path); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(path, "SKILL.md")
		if err := rejectSymlinkComponents(root, skillPath); err != nil {
			return nil, err
		}
		info, err := os.Lstat(skillPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("skill content must be a regular file")
		}
		data, err := readBoundedFile(skillPath, 64<<10)
		if err != nil {
			return nil, err
		}
		skills = append(skills, Skill{
			Name:    entry.Name(),
			Path:    filepath.ToSlash(filepath.Join(".aicli", "skills", entry.Name(), "SKILL.md")),
			Content: string(data),
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func loadMarkdownDirectory(root, dir string) ([]Rule, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var rules []Rule
	for _, entry := range entries {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") == false {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := rejectSymlinkComponents(root, path); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("rule content must be a regular file")
		}
		data, err := readBoundedFile(path, 64<<10)
		if err != nil {
			return nil, err
		}
		rules = append(rules, Rule{
			Name:    strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Content: string(data),
		})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })
	return rules, nil
}

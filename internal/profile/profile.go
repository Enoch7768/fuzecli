package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"fuzecli/internal/config"
	"fuzecli/internal/provider"
)

type Profile struct {
	PreferredLanguages []string          `json:"preferred_languages"`
	CodingStyle        map[string]string `json:"coding_style"`
	RecurringPatterns  []string          `json:"recurring_patterns"`
	LastUpdated        string            `json:"last_updated"`
}

func Default() Profile {
	return Profile{PreferredLanguages: []string{}, CodingStyle: map[string]string{}, RecurringPatterns: []string{}}
}
func Load() (Profile, error) {
	b, err := os.ReadFile(config.ProfilePath())
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return Profile{}, err
	}
	if p.CodingStyle == nil {
		p.CodingStyle = map[string]string{}
	}
	return p, nil
}
func Save(p Profile) error {
	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		return err
	}
	p.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.MarshalIndent(p, "", "  ")
	tmp := config.ProfilePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, config.ProfilePath())
}
func (p Profile) Condensed() string { b, _ := json.Marshal(p); return string(b) }
func (p *Profile) MergeFacts(facts []string) {
	languages := []string{"Go", "PHP", "JavaScript", "TypeScript", "Python", "Java", "C#", "C++", "Rust", "Ruby", "Kotlin", "Swift", "SQL"}
	for _, fact := range facts {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			continue
		}
		found := false
		for _, x := range p.RecurringPatterns {
			if similarity(x, fact) >= 0.6 {
				found = true
				break
			}
		}
		if !found {
			p.RecurringPatterns = append(p.RecurringPatterns, fact)
		}
		lower := strings.ToLower(fact)
		for _, lang := range languages {
			if strings.Contains(lower, strings.ToLower(lang)) && !containsFold(p.PreferredLanguages, lang) {
				p.PreferredLanguages = append(p.PreferredLanguages, lang)
			}
		}
	}
	sort.Strings(p.RecurringPatterns)
	sort.Strings(p.PreferredLanguages)
	p.LastUpdated = time.Now().UTC().Format(time.RFC3339)
}

func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}
func similarity(a, b string) float64 {
	ta := tokenSet(a)
	tb := tokenSet(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for t := range ta {
		if tb[t] {
			inter++
		}
	}
	return float64(inter) / float64(len(ta)+len(tb)-inter)
}
func tokenSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, t := range strings.Fields(strings.ToLower(s)) {
		t = strings.Trim(t, ".,:;()[]{}\"'")
		if len(t) > 2 {
			m[t] = true
		}
	}
	return m
}
func ExtractAndMerge(ctx context.Context, p *Profile, reg *provider.Registry, prov string, model string, conversation []provider.Message) error {
	summary := strings.Builder{}
	for _, m := range conversation {
		if m.Role == "user" || m.Role == "assistant" {
			fmt.Fprintf(&summary, "%s: %s\n", m.Role, m.Content)
		}
	}
	msgs := []provider.Message{{Role: "system", Content: `Given this conversation, list durable facts about this developer's coding preferences and patterns worth remembering long-term. Return only JSON: {"facts":["..."]}. Do not include personal secrets or transient task details.`}, {Role: "user", Content: summary.String()}}
	resp, err := reg.Send(ctx, prov, msgs, provider.RequestOptions{Model: model, Temperature: 0, MaxTokens: 800})
	if err != nil {
		return err
	}
	var out struct {
		Facts []string `json:"facts"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return fmt.Errorf("profile extraction returned invalid JSON: %w", err)
	}
	p.MergeFacts(out.Facts)
	return Save(*p)
}

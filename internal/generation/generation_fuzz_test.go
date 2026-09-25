package generation

import "testing"

func FuzzParsePlanNeverPanics(f *testing.F) {
	seeds := []string{
		"{\"files\":[{\"path\":\"main.go\",\"content\":\"package main\",\"action\":\"create\"}],\"explanation\":\"ok\",\"commands\":[]}",
		"{\"files\":[{\"path\":\"../escape\",\"content\":\"x\",\"action\":\"create\"}]}",
		"{\"files\":[{\"path\":\"C:\\\\escape\",\"content\":\"x\",\"action\":\"create\"}]}",
		"{\"files\":[{\"path\":\"x\",\"content\":\"{\",\"action\":\"modify\"}]}",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParsePlan(input)
	})
}

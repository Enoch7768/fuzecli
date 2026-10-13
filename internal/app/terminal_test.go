package app

import "testing"

func TestTerminalPromptUsesPlainChatUnlessChangeIsRequested(t *testing.T) {
	cases := []struct {
		prompt string
		want   bool
	}{
		{prompt: "Hello, how are you?", want: false},
		{prompt: "Explain how Go interfaces work", want: false},
		{prompt: "Analyze this code and recommend improvements", want: false},
		{prompt: "Fix the bug in main.go", want: true},
		{prompt: "Create a new login page", want: true},
		{prompt: "Refactor the authentication module", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.prompt, func(t *testing.T) {
			if got := terminalPromptNeedsApproval(tc.prompt); got != tc.want {
				t.Fatalf("terminalPromptNeedsApproval(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}

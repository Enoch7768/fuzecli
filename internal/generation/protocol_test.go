package generation

import "testing"

func TestValidateProtocolResponse(t *testing.T) {
	cases := []struct {
		name string
		response ProtocolResponse
		ok bool
	}{
		{"chat", ProtocolResponse{Type: ResponseChat, Response: "hello"}, true},
		{"analysis", ProtocolResponse{Type: ResponseAnalysis, Explanation: "findings"}, true},
		{"edit", ProtocolResponse{Type: ResponseEdit, Files: []FileChange{{Path: "main.go", Action: "modify", Content: "package main"}}}, true},
		{"command", ProtocolResponse{Type: ResponseCommand, Commands: []string{"go test ./..."}}, true},
		{"error", ProtocolResponse{Type: ResponseError, Error: "failed"}, true},
		{"progress", ProtocolResponse{Type: ResponseProgress, Progress: 50}, true},
		{"missing edit", ProtocolResponse{Type: ResponseEdit}, false},
		{"invalid action", ProtocolResponse{Type: ResponseEdit, Files: []FileChange{{Path: "x", Action: "run"}}}, false},
		{"invalid progress", ProtocolResponse{Type: ResponseProgress, Progress: 101}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProtocolResponse(tc.response)
			if (err == nil) != tc.ok {
				t.Fatalf("validation error = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestProtocolRoundTrip(t *testing.T) {
	raw, err := EncodeProtocolResponse(ProtocolResponse{
		Type: ResponseEdit,
		Files: []FileChange{{Path: "index.html", Action: "modify", Content: "<main>ok</main>"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseChatResponse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "edit" || len(parsed.Files) != 1 || parsed.Files[0].Path != "index.html" {
		t.Fatalf("unexpected parsed response: %#v", parsed)
	}
}

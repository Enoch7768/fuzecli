package llamacpp

import "testing"

func TestNewNormalizesOpenAICompatibleBaseURL(t *testing.T) {
	tests := []struct {
		name string
		baseURL string
		want string
	}{
		{name: "default endpoint", baseURL: "", want: "http://localhost:8080/v1"},
		{name: "root endpoint", baseURL: "http://127.0.0.1:8080/", want: "http://127.0.0.1:8080/v1"},
		{name: "already includes v1", baseURL: "http://127.0.0.1:8080/v1/", want: "http://127.0.0.1:8080/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.baseURL).inner.BaseURL
			if got != tt.want {
				t.Fatalf("base URL = %q, want %q", got, tt.want)
			}
		})
	}
}

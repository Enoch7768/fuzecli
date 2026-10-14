package api

import (
	"strings"
	"sync"
	"testing"
)

func TestRunSpecRecognizesCommonLanguages(t *testing.T) {
	tests := []struct {
		extension string
		runtime   string
	}{
		{".js", "node"},
		{".py", "python"},
		{".php", "php"},
		{".go", "go"},
		{".java", "java"},
		{".rb", "ruby"},
		{".pl", "perl"},
		{".lua", "lua"},
		{".swift", "swift"},
		{".dart", "dart"},
		{".exs", "elixir"},
		{".hs", "runghc"},
		{".scala", "scala"},
		{".jsx", "Node.js + tsx"},
		{".mts", "Node.js + tsx"},
		{".cts", "Node.js + tsx"},
		{".c++", "C/C++ compiler"},
		{".f90", "Fortran compiler"},
		{".pas", "Pascal compiler"},
		{".d", "D compiler"},
		{".zig", "zig"},
		{".nim", "nim"},
		{".clj", "clojure"},
		{".groovy", "groovy"},
		{".cr", "crystal"},
		{".rkt", "racket"},
		{".fsx", "dotnet"},
	}
	for _, test := range tests {
		t.Run(test.extension, func(t *testing.T) {
			_, _, runtimeName, _, err := runSpec(t.TempDir(), "/workspace/main"+test.extension, test.extension)
			if err != nil {
				t.Fatalf("runSpec returned error: %v", err)
			}
			if runtimeName == "" {
				t.Fatal("runtime name is empty")
			}
			if test.extension != ".py" && runtimeName != test.runtime {
				t.Fatalf("runtime = %q, want %q", runtimeName, test.runtime)
			}
		})
	}
}

func TestRunSpecRejectsUnsupportedFiles(t *testing.T) {
	_, _, _, _, err := runSpec(t.TempDir(), "/workspace/notes.txt", ".txt")
	if err == nil {
		t.Fatal("expected unsupported extension to return an error")
	}
}

func TestBoundedBufferCapsConcurrentOutput(t *testing.T) {
	buffer := &boundedBuffer{limit: 1024}
	var workers sync.WaitGroup
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				_, _ = buffer.Write([]byte(strings.Repeat("x", 32)))
			}
		}()
	}
	workers.Wait()
	if got := len(buffer.String()); got > buffer.limit {
		t.Fatalf("buffer length = %d, limit = %d", got, buffer.limit)
	}
}

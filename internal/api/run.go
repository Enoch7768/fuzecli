package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type runFileRequest struct {
	Path string `json:"path"`
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.Buffer.Write(p[:remaining])
	}
	return n, nil
}

func (s *Server) runFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var req runFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid run request")
		return
	}
	name := strings.TrimSpace(req.Path)
	if name == "" || filepath.IsAbs(name) {
		writeError(w, http.StatusBadRequest, "select a workspace file to run")
		return
	}
	root, err := filepath.Abs(s.service.Root())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace path is unavailable")
		return
	}
	source := filepath.Clean(filepath.Join(root, filepath.FromSlash(name)))
	rel, err := filepath.Rel(root, source)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		writeError(w, http.StatusForbidden, "file is outside the workspace")
		return
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "workspace file not found")
		return
	}
	if info.Size() > 8<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "source file exceeds the 8 MiB run limit")
		return
	}
	ext := strings.ToLower(filepath.Ext(source))
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out := &boundedBuffer{limit: 1 << 20}
	errOut := &boundedBuffer{limit: 1 << 20}
	command, args, runtimeName, build, err := runSpec(root, source, ext)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if _, err := exec.LookPath(command); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime_missing": true, "runtime": runtimeName, "message": runtimeName + " is not installed or is not available on PATH."})
		return
	}
	if build != nil {
		buildCmd, buildArgs := build()
		buildCmd.Dir = root
		buildCmd.Stdout = out
		buildCmd.Stderr = errOut
		if err := buildCmd.Run(); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime": runtimeName, "output": out.String() + errOut.String(), "message": "Build failed: " + err.Error()})
			return
		}
		command, args = buildArgs[0], buildArgs[1:]
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	cmd.Stdout = out
	cmd.Stderr = errOut
	start := time.Now()
	err = cmd.Run()
	output := out.String() + errOut.String()
	if ctx.Err() == context.DeadlineExceeded {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime": runtimeName, "output": output, "message": "Execution exceeded the 2-minute limit."})
		return
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime": runtimeName, "exit_code": exitErr.ExitCode(), "duration_ms": time.Since(start).Milliseconds(), "output": output, "message": "Program exited with a non-zero status."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime": runtimeName, "output": output, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runtime": runtimeName, "exit_code": 0, "duration_ms": time.Since(start).Milliseconds(), "output": output})
}

func runSpec(root, source, ext string) (string, []string, string, func() (*exec.Cmd, []string), error) {
	noBuild := func(command string, args ...string) (string, []string, string, func() (*exec.Cmd, []string), error) {
		return command, args, command, nil, nil
	}
	switch ext {
	case ".js", ".mjs", ".cjs":
		return noBuild("node", source)
	case ".ts", ".tsx":
		if _, err := exec.LookPath("tsx"); err == nil { return noBuild("tsx", source) }
		return "", nil, "Node.js + tsx", nil, nil
	case ".py", ".pyw":
		cmd := "python"
		if runtime.GOOS != "windows" { if _, err := exec.LookPath("python3"); err == nil { cmd = "python3" } }
		return noBuild(cmd, source)
	case ".php":
		return noBuild("php", source)
	case ".go":
		return noBuild("go", "run", source)
	case ".rs":
		tmp, err := os.MkdirTemp("", "fuzecli-rust-*")
		if err != nil { return "", nil, "", nil, err }
		bin := filepath.Join(tmp, "program")
		if runtime.GOOS == "windows" { bin += ".exe" }
		build := func() (*exec.Cmd, []string) { c := exec.Command("rustc", source, "-o", bin); return c, []string{bin} }
		return "rustc", []string{source, "-o", bin}, "Rust (rustc)", build, nil
	case ".c", ".cc", ".cpp", ".cxx":
		compiler := "cc"
		if runtime.GOOS == "windows" { compiler = "gcc" }
		for _, candidate := range []string{compiler, "clang", "gcc", "g++"} { if _, err := exec.LookPath(candidate); err == nil { compiler = candidate; break } }
		tmp, err := os.MkdirTemp("", "fuzecli-native-*")
		if err != nil { return "", nil, "", nil, err }
		bin := filepath.Join(tmp, "program")
		if runtime.GOOS == "windows" { bin += ".exe" }
		build := func() (*exec.Cmd, []string) { c := exec.Command(compiler, source, "-o", bin); return c, []string{bin} }
		return compiler, []string{source, "-o", bin}, "C/C++ compiler", build, nil
	case ".java":
		return noBuild("java", source)
	case ".kt", ".kts":
		tmp, err := os.MkdirTemp("", "fuzecli-kotlin-*")
		if err != nil { return "", nil, "", nil, err }
		jar := filepath.Join(tmp, "program.jar")
		build := func() (*exec.Cmd, []string) { c := exec.Command("kotlinc", source, "-include-runtime", "-d", jar); return c, []string{"java", "-jar", jar} }
		return "kotlinc", []string{source, "-include-runtime", "-d", jar}, "Kotlin compiler + Java", build, nil
	case ".cs":
		if _, err := os.Stat(filepath.Join(root, ".*.csproj")); err == nil { return noBuild("dotnet", "run") }
		return "", nil, ".NET SDK (open a project containing a .csproj file)", nil, nil
	case ".rb":
		return noBuild("ruby", source)
	case ".pl", ".pm":
		return noBuild("perl", source)
	case ".lua":
		return noBuild("lua", source)
	case ".r":
		return noBuild("Rscript", source)
	case ".jl":
		return noBuild("julia", source)
	case ".swift":
		return noBuild("swift", source)
	case ".dart":
		return noBuild("dart", source)
	case ".ex", ".exs":
		return noBuild("elixir", source)
	case ".hs", ".lhs":
		return noBuild("runghc", source)
	case ".scala":
		return noBuild("scala", source)
	case ".sh", ".bash":
		if runtime.GOOS == "windows" { return noBuild("bash", source) }
		return noBuild("sh", source)
	case ".ps1":
		if runtime.GOOS == "windows" { return noBuild("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", source) }
		return noBuild("pwsh", "-NoProfile", "-File", source)
	default:
		return "", nil, "", nil, fmt.Errorf("running %s is not configured yet; use the terminal or choose a supported source file", ext)
	}
}

var _ = fmt.Sprintf

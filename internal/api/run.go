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
	"sync"
	"time"
)

type runFileRequest struct {
	Path string `json:"path"`
}

type boundedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
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
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace file not found")
		return
	}
	resolvedRel, err := filepath.Rel(root, resolved)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(os.PathSeparator)) {
		writeError(w, http.StatusForbidden, "symlink resolves outside the workspace")
		return
	}
	source = resolved
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
	if tempDir := temporaryBuildDirectory(args); tempDir != "" { defer os.RemoveAll(tempDir) }
	if _, err := exec.LookPath(command); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime_missing": true, "runtime": runtimeName, "message": runtimeName + " is not installed or is not available on PATH."})
		return
	}
	if build != nil {
		buildCmd, buildArgs := build()
		buildCmd = exec.CommandContext(ctx, buildCmd.Path, buildCmd.Args[1:]...)
		buildCmd.Dir = root
		buildCmd.Env = os.Environ()
		buildCmd.Stdout = out
		buildCmd.Stderr = errOut
		if err := buildCmd.Run(); err != nil {
			message := "Build failed: " + err.Error()
			if ctx.Err() == context.DeadlineExceeded { message = "Build exceeded the 2-minute execution limit." }
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "runtime": runtimeName, "output": out.String() + errOut.String(), "message": message})
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

func temporaryBuildDirectory(args []string) string {
	if len(args) >= 4 && strings.HasPrefix(args[0], "-FE") && strings.HasPrefix(args[1], "-FU") {
		dir := strings.TrimPrefix(args[0], "-FE")
		if isFuzeTemporaryDirectory(dir, "fuzecli-pascal-") { return dir }
		return ""
	}
	var output, requiredPrefix string
	switch {
	case len(args) >= 3 && args[1] == "-o":
		output = args[2]
	case len(args) >= 4 && args[1] == "-include-runtime" && args[2] == "-d":
		output, requiredPrefix = args[3], "fuzecli-kotlin-"
	case len(args) >= 3 && args[0] == "run" && args[1] == "--project":
		output, requiredPrefix = args[2], "fuzecli-csharp-"
	default:
		return ""
	}
	dir := filepath.Dir(output)
	prefix := filepath.Base(dir)
	if requiredPrefix != "" {
		if isFuzeTemporaryDirectory(dir, requiredPrefix) { return dir }
		return ""
	}
	if isFuzeTemporaryDirectory(dir, "fuzecli-rust-") || isFuzeTemporaryDirectory(dir, "fuzecli-native-") || isFuzeTemporaryDirectory(dir, "fuzecli-compile-") { return dir }
	return ""
}

func isFuzeTemporaryDirectory(dir, prefix string) bool {
	if !strings.HasPrefix(filepath.Base(dir), prefix) { return false }
	tempRoot, err := filepath.Abs(os.TempDir())
	if err != nil { return false }
	absolute, err := filepath.Abs(dir)
	if err != nil { return false }
	relative, err := filepath.Rel(tempRoot, absolute)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func compiledSpec(root, source, compiler, runtimeName, outputFlag string) (string, []string, string, func() (*exec.Cmd, []string), error) {
	tmp, err := os.MkdirTemp("", "fuzecli-compile-*")
	if err != nil { return "", nil, "", nil, err }
	bin := filepath.Join(tmp, strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)))
	if runtime.GOOS == "windows" { bin += ".exe" }
	build := func() (*exec.Cmd, []string) {
		cmd := exec.Command(compiler, source, outputFlag, bin)
		return cmd, []string{bin}
	}
	return compiler, []string{source, outputFlag, bin}, runtimeName, build, nil
}

func pascalSpec(source string) (string, []string, string, func() (*exec.Cmd, []string), error) {
	tmp, err := os.MkdirTemp("", "fuzecli-pascal-*")
	if err != nil { return "", nil, "", nil, err }
	bin := filepath.Join(tmp, strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)))
	if runtime.GOOS == "windows" { bin += ".exe" }
	args := []string{"-FE" + tmp, "-FU" + tmp, "-o" + bin, source}
	build := func() (*exec.Cmd, []string) { return exec.Command("fpc", args...), []string{bin} }
	return "fpc", args, "Pascal compiler", build, nil
}

func runSpec(root, source, ext string) (string, []string, string, func() (*exec.Cmd, []string), error) {
	noBuild := func(command string, args ...string) (string, []string, string, func() (*exec.Cmd, []string), error) {
		return command, args, command, nil, nil
	}
	switch ext {
	case ".js", ".mjs", ".cjs":
		return noBuild("node", source)
	case ".ts", ".tsx", ".jsx", ".mts", ".cts":
		if _, err := exec.LookPath("tsx"); err == nil { return "tsx", []string{source}, "Node.js + tsx", nil, nil }
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
	case ".c", ".cc", ".cpp", ".cxx", ".c++":
		compiler := "cc"
		cpp := ext == ".cc" || ext == ".cpp" || ext == ".cxx" || ext == ".c++" || ext == ".hpp"
		candidates := []string{"cc", "clang", "gcc"}
		if cpp { candidates = []string{"g++", "clang++", "c++"} }
		if runtime.GOOS == "windows" && !cpp { candidates = []string{"gcc", "clang", "cc"} }
		foundCompiler := false
		for _, candidate := range candidates { if _, err := exec.LookPath(candidate); err == nil { compiler = candidate; foundCompiler = true; break } }
		if !foundCompiler { return "", nil, "C/C++ compiler", nil, nil }
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
		if projects, _ := filepath.Glob(filepath.Join(root, "*.csproj")); len(projects) > 0 { return noBuild("dotnet", "run", "--project", projects[0]) }
		tmp, err := os.MkdirTemp("", "fuzecli-csharp-*")
		if err != nil { return "", nil, "", nil, err }
		data, err := os.ReadFile(source)
		if err != nil { _ = os.RemoveAll(tmp); return "", nil, "", nil, fmt.Errorf("read C# source: %w", err) }
		program := filepath.Join(tmp, "Program.cs")
		project := filepath.Join(tmp, "Program.csproj")
		if err := os.WriteFile(program, data, 0600); err != nil { _ = os.RemoveAll(tmp); return "", nil, "", nil, fmt.Errorf("prepare C# source: %w", err) }
		projectData := []byte("<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings><Nullable>enable</Nullable></PropertyGroup></Project>")
		if err := os.WriteFile(project, projectData, 0600); err != nil { _ = os.RemoveAll(tmp); return "", nil, "", nil, fmt.Errorf("prepare C# project: %w", err) }
		return noBuild("dotnet", "run", "--project", project, "--no-launch-profile")
	case ".rb":
		return noBuild("ruby", source)
	case ".fsx":
		return noBuild("dotnet", "fsi", source)
	case ".f", ".for", ".f90", ".f95", ".f03", ".f08":
		return compiledSpec(root, source, "gfortran", "Fortran compiler", "-o")
	case ".pas", ".pp":
		return pascalSpec(source)
	case ".d":
		if _, err := exec.LookPath("dmd"); err == nil { return "dmd", []string{"-run", source}, "D compiler", nil, nil }
		if _, err := exec.LookPath("ldc2"); err == nil { return "ldc2", []string{"-run", source}, "D compiler", nil, nil }
		if _, err := exec.LookPath("gdc"); err == nil { return compiledSpec(root, source, "gdc", "D compiler", "-o") }
		return "", nil, "D compiler", nil, nil
	case ".zig":
		return noBuild("zig", "run", source)
	case ".nim":
		return noBuild("nim", "r", "--hints:off", source)
	case ".clj":
		return noBuild("clojure", source)
	case ".groovy":
		return noBuild("groovy", source)
	case ".cr":
		return noBuild("crystal", "run", source)
	case ".rkt":
		return noBuild("racket", source)
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


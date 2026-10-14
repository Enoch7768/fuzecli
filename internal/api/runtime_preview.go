package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type RuntimePreviewInfo struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type runtimePreviewManager struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	info RuntimePreviewInfo
}

func (m *runtimePreviewManager) Start(root, requested string) (RuntimePreviewInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}

	kind, err := detectRuntime(root, requested)
	if err != nil {
		return RuntimePreviewInfo{}, err
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return RuntimePreviewInfo{}, err
	}
	command, args, env, err := runtimeCommand(root, kind, port)
	if err != nil {
		return RuntimePreviewInfo{}, err
	}
	if _, err := exec.LookPath(command); err != nil {
		return RuntimePreviewInfo{}, fmt.Errorf("%s runtime is not installed or is not available on PATH", kind)
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	output := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return RuntimePreviewInfo{}, fmt.Errorf("start %s preview: %w: %s", kind, err, strings.TrimSpace(output.String()))
	}
	m.cmd = cmd
	m.info = RuntimePreviewInfo{Kind: kind, URL: fmt.Sprintf("http://127.0.0.1:%d/", port)}

	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
		}
		m.mu.Unlock()
	}()

	if err := waitForPreview(port, 8*time.Second); err != nil {
		_ = cmd.Process.Kill()
		m.cmd = nil
		logs := strings.TrimSpace(output.String())
		if logs != "" { return RuntimePreviewInfo{}, fmt.Errorf("%s preview did not become ready: %w. Startup output: %s", kind, err, logs) }
		return RuntimePreviewInfo{}, fmt.Errorf("%s preview did not become ready: %w. Check the app entry point and install its project dependencies, then retry.", kind, err)
	}
	return m.info, nil
}

func (m *runtimePreviewManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	m.cmd = nil
	m.info = RuntimePreviewInfo{}
}

func detectRuntime(root, requested string) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested != "" && requested != "auto" {
		switch requested {
		case "php", "node", "python":
			return requested, nil
		default:
			return "", fmt.Errorf("unsupported preview runtime %q", requested)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		return "node", nil
	}
	for _, entry := range []string{"server.js", "index.js", "app.js", "server.mjs", "index.mjs"} {
		if _, err := os.Stat(filepath.Join(root, entry)); err == nil { return "node", nil }
	}
	if _, err := os.Stat(filepath.Join(root, "manage.py")); err == nil {
		return "python", nil
	}
	if data, err := os.ReadFile(filepath.Join(root, "requirements.txt")); err == nil {
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "flask") || strings.Contains(lower, "fastapi") || strings.Contains(lower, "django") {
			return "python", nil
		}
	}
	if _, err := os.Stat(filepath.Join(root, "app.py")); err == nil {
		return "python", nil
	}
	if _, err := os.Stat(filepath.Join(root, "main.py")); err == nil {
		return "python", nil
	}
	foundPHP := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if strings.EqualFold(filepath.Ext(path), ".php") {
			foundPHP = true
			return filepath.SkipAll
		}
		return nil
	})
	if foundPHP {
		return "php", nil
	}
	return "", errors.New("no runnable PHP, Node, or Python application was detected")
}

func runtimeCommand(root, kind string, port int) (string, []string, []string, error) {
	hostPort := fmt.Sprintf("127.0.0.1:%d", port)
	switch kind {
	case "php":
		publicRoot := filepath.Join(root, "public")
		if info, err := os.Stat(filepath.Join(publicRoot, "index.php")); err == nil && !info.IsDir() {
			return "php", []string{"-S", hostPort, "-t", publicRoot, filepath.Join(publicRoot, "index.php")}, nil, nil
		}
		return "php", []string{"-S", hostPort, "-t", root}, nil, nil
	case "node":
		data, err := os.ReadFile(filepath.Join(root, "package.json"))
		if err != nil {
			for _, entry := range []string{"server.js", "index.js", "app.js", "server.mjs", "index.mjs"} {
				if _, statErr := os.Stat(filepath.Join(root, entry)); statErr == nil {
					return "node", []string{entry}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
				}
			}
			return "", nil, nil, errors.New("Node project has no package.json or recognizable server entry point")
		}
		var pkg struct { Scripts map[string]string `json:"scripts"` }
		if err := json.Unmarshal(data, &pkg); err != nil { return "", nil, nil, fmt.Errorf("parse package.json: %w", err) }
		script := "dev"
		if _, ok := pkg.Scripts[script]; !ok { script = "start" }
		if _, ok := pkg.Scripts[script]; !ok { return "", nil, nil, errors.New("package.json has neither a dev nor start script") }
		npm := "npm"
		if runtime.GOOS == "windows" { npm = "npm.cmd" }
		args := []string{"run", script}
		commandText := strings.ToLower(pkg.Scripts[script])
		if strings.Contains(commandText, "vite") {
			args = append(args, "--", "--host", "127.0.0.1", "--port", fmt.Sprint(port))
		} else if strings.Contains(commandText, "next dev") {
			args = append(args, "--", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port))
		} else if strings.Contains(commandText, "astro dev") {
			args = append(args, "--", "--host", "127.0.0.1", "--port", fmt.Sprint(port))
		}
		return npm, args, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
	case "python":
		requirements, _ := os.ReadFile(filepath.Join(root, "requirements.txt"))
		lower := strings.ToLower(string(requirements))
		if _, err := os.Stat(filepath.Join(root, "manage.py")); err == nil {
			return pythonExecutable(), []string{"manage.py", "runserver", hostPort}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		entry := "app.py"
		if _, err := os.Stat(filepath.Join(root, entry)); err != nil { entry = "main.py" }
		if strings.Contains(lower, "fastapi") || strings.Contains(lower, "uvicorn") {
			module := strings.TrimSuffix(filepath.Base(entry), filepath.Ext(entry)) + ":app"
			return pythonExecutable(), []string{"-m", "uvicorn", module, "--host", "127.0.0.1", "--port", fmt.Sprint(port)}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		if strings.Contains(lower, "flask") {
			return pythonExecutable(), []string{"-m", "flask", "--app", entry, "run", "--host", "127.0.0.1", "--port", fmt.Sprint(port)}, []string{"FLASK_RUN_HOST=127.0.0.1", "FLASK_RUN_PORT=" + fmt.Sprint(port), "HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		if data, err := os.ReadFile(filepath.Join(root, entry)); err == nil {
			lowerSource := strings.ToLower(string(data))
			if strings.Contains(lowerSource, "uvicorn.run(") || strings.Contains(lowerSource, "app.run(") || strings.Contains(lowerSource, "http.server") {
				return pythonExecutable(), []string{entry}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
			}
		}
		return pythonExecutable(), []string{"-m", "http.server", fmt.Sprint(port), "--bind", "127.0.0.1"}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
	default:
		return "", nil, nil, fmt.Errorf("unsupported preview runtime %q", kind)
	}
}

func pythonExecutable() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	if _, err := exec.LookPath("python3"); err == nil {
		return "python3"
	}
	return "python"
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForPreview(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp4", address, 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("runtime did not open its loopback port")
}

func runtimePreviewHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

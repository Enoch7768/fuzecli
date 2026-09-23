package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return RuntimePreviewInfo{}, fmt.Errorf("start %s preview: %w", kind, err)
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

	if err := waitForPreview(port, 6*time.Second); err != nil {
		_ = cmd.Process.Kill()
		m.cmd = nil
		return RuntimePreviewInfo{}, fmt.Errorf("%s preview did not become ready: %w", kind, err)
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
		return "php", []string{"-S", hostPort, "-t", root}, nil, nil
	case "node":
		data, err := os.ReadFile(filepath.Join(root, "package.json"))
		if err != nil {
			return "", nil, nil, fmt.Errorf("read package.json: %w", err)
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return "", nil, nil, fmt.Errorf("parse package.json: %w", err)
		}
		script := "dev"
		if _, ok := pkg.Scripts[script]; !ok {
			script = "start"
		}
		if _, ok := pkg.Scripts[script]; !ok {
			return "", nil, nil, errors.New("package.json has neither a dev nor start script")
		}
		npm := "npm"
		if runtime.GOOS == "windows" {
			npm = "npm.cmd"
		}
		return npm, []string{"run", script, "--", "--host", "127.0.0.1", "--port", fmt.Sprint(port)}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
	case "python":
		requirements, _ := os.ReadFile(filepath.Join(root, "requirements.txt"))
		lower := strings.ToLower(string(requirements))
		if _, err := os.Stat(filepath.Join(root, "manage.py")); err == nil {
			return pythonExecutable(), []string{"manage.py", "runserver", hostPort}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		if strings.Contains(lower, "fastapi") {
			return pythonExecutable(), []string{"-m", "uvicorn", "app:app", "--host", "127.0.0.1", "--port", fmt.Sprint(port)}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		if strings.Contains(lower, "flask") {
			return pythonExecutable(), []string{"-m", "flask", "--app", "app", "run", "--host", "127.0.0.1", "--port", fmt.Sprint(port)}, []string{"FLASK_RUN_HOST=127.0.0.1", "FLASK_RUN_PORT=" + fmt.Sprint(port), "HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		entry := "app.py"
		if _, err := os.Stat(filepath.Join(root, entry)); err != nil {
			entry = "main.py"
		}
		if _, err := os.Stat(filepath.Join(root, entry)); err != nil {
			return pythonExecutable(), []string{"-m", "http.server", fmt.Sprint(port), "--bind", "127.0.0.1"}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
		}
		return pythonExecutable(), []string{entry}, []string{"HOST=127.0.0.1", "PORT=" + fmt.Sprint(port)}, nil
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

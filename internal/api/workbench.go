package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type terminalManager struct {
	mu sync.Mutex
	sessions map[string]*terminalSession
}

type terminalSession struct {
	id string
	cmd *exec.Cmd
	stdin io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	cancel context.CancelFunc
}

func newTerminalManager() *terminalManager {
	return &terminalManager{sessions: make(map[string]*terminalSession)}
}

func (m *terminalManager) start(root string) (*terminalSession, error) {
	ctx, cancel := context.WithCancel(context.Background())
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", "cmd.exe")
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-lc", "exec $SHELL -i")
	}
	cmd.Dir = root
	cmd.Env = os.Environ()
	in, err := cmd.StdinPipe()
	if err != nil { cancel(); return nil, err }
	stdout, err := cmd.StdoutPipe()
	if err != nil { cancel(); return nil, err }
	stderr, err := cmd.StderrPipe()
	if err != nil { cancel(); return nil, err }
	if err := cmd.Start(); err != nil { cancel(); return nil, err }
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
	s := &terminalSession{id: id, cmd: cmd, stdin: in, stdout: stdout, stderr: stderr, cancel: cancel}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		cancel()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()
	return s, nil
}

func (m *terminalManager) stop(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s != nil {
		_ = s.stdin.Close()
		s.cancel()
	}
}

type lspSpec struct {
	Command string
	Args []string
}

func lspForLanguage(language string) (lspSpec, bool) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "go": return lspSpec{"gopls", []string{"serve"}}, true
	case "typescript", "javascript": return lspSpec{"typescript-language-server", []string{"--stdio"}}, true
	case "python": return lspSpec{"pyright-langserver", []string{"--stdio"}}, true
	case "rust": return lspSpec{"rust-analyzer", nil}, true
	case "php": return lspSpec{"intelephense", []string{"--stdio"}}, true
	case "java": return lspSpec{"jdtls", nil}, true
	case "c", "cpp": return lspSpec{"clangd", nil}, true
	case "ruby": return lspSpec{"ruby-lsp", nil}, true
	case "kotlin": return lspSpec{"kotlin-language-server", nil}, true
	default: return lspSpec{}, false
	}
}

func startLanguageServer(root, language string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	spec, ok := lspForLanguage(language)
	if !ok { return nil, nil, nil, fmt.Errorf("no configured language server for %s", language) }
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	in, err := cmd.StdinPipe()
	if err != nil { return nil, nil, nil, err }
	out, err := cmd.StdoutPipe()
	if err != nil { return nil, nil, nil, err }
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil { return nil, nil, nil, fmt.Errorf("start %s: %w", spec.Command, err) }
	return cmd, in, out, nil
}

func readLSPMessage(br *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil { return nil, err }
		line = strings.TrimSpace(line)
		if line == "" { break }
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			length, _ = strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
		}
	}
	if length <= 0 || length > 32<<20 { return nil, errors.New("invalid LSP message length") }
	buf := make([]byte, length)
	_, err := io.ReadFull(br, buf)
	return buf, err
}

func writeLSPMessage(w io.Writer, msg []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(msg)); err != nil { return err }
	_, err := io.Copy(w, bytes.NewReader(msg))
	return err
}

func bridgeLSP(ws *websocket.Conn, root, language string) error {
	cmd, in, out, err := startLanguageServer(root, language)
	if err != nil { return err }
	defer cmd.Process.Kill()
	go func() {
		br := bufio.NewReader(out)
		for {
			msg, err := readLSPMessage(br)
			if err != nil { return }
			_ = ws.WriteMessage(websocket.TextMessage, msg)
		}
	}()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil { return err }
		if json.Valid(msg) {
			if err := writeLSPMessage(in, msg); err != nil { return err }
		}
	}
}

func startDAP(root string) (net.Conn, *exec.Cmd, error) {
	cmd := exec.Command("dlv", "dap", "--listen=127.0.0.1:0")
	cmd.Dir = root
	cmd.Env = os.Environ()
	stderr, err := cmd.StderrPipe()
	if err != nil { return nil, nil, err }
	if err := cmd.Start(); err != nil { return nil, nil, fmt.Errorf("start delve: %w", err) }
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, "127.0.0.1:")
		if i >= 0 {
			addr := strings.Fields(strings.TrimSpace(line[i:]))[0]
			conn, dialErr := net.DialTimeout("tcp", addr, 3*time.Second)
			if dialErr == nil { return conn, cmd, nil }
		}
	}
	_ = cmd.Process.Kill()
	return nil, nil, errors.New("Delve did not expose a DAP listener; install Delve and ensure dlv is on PATH")
}

func bridgeDAP(ws *websocket.Conn, root string) error {
	conn, cmd, err := startDAP(root)
	if err != nil { return err }
	defer cmd.Process.Kill()
	defer conn.Close()
	go func() {
		br := bufio.NewReader(conn)
		for {
			msg, err := readLSPMessage(br)
			if err != nil { return }
			_ = ws.WriteMessage(websocket.TextMessage, msg)
		}
	}()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil { return err }
		if json.Valid(msg) {
			if err := writeLSPMessage(conn, msg); err != nil { return err }
		}
	}
}

type workbenchRuntime struct {
	terminal *terminalManager
	upgrader websocket.Upgrader
}

func newWorkbenchRuntime() *workbenchRuntime {
	return &workbenchRuntime{
		terminal: newTerminalManager(),
		upgrader: websocket.Upgrader{
			ReadBufferSize: 8192,
			WriteBufferSize: 8192,
			CheckOrigin: func(r *http.Request) bool { return isLoopbackRequest(r) || strings.TrimSpace(r.Header.Get("Origin")) == "" },
		},
	}
}

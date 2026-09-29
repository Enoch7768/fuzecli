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
	"path/filepath"
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
	defer func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Process.Kill()
	}()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		br := bufio.NewReader(out)
		for {
			msg, err := readLSPMessage(br)
			if err != nil { return }
			if err := ws.WriteMessage(websocket.TextMessage, msg); err != nil { return }
		}
	}()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-readerDone:
			case <-time.After(500 * time.Millisecond):
			}
			return err
		}
		if !json.Valid(msg) { continue }
		if err := writeLSPMessage(in, msg); err != nil { return err }
	}
}

type debugAdapterSpec struct {
	Command string
	Args []string
	LaunchMode string
}

func debugAdapterForLanguage(language string) (debugAdapterSpec, error) {
	lang := strings.ToLower(strings.TrimSpace(language))
	envName := "FUZECLI_DEBUG_ADAPTER_" + strings.ToUpper(strings.ReplaceAll(lang, "-", "_"))
	if command := strings.TrimSpace(os.Getenv(envName)); command != "" {
		args := strings.Fields(strings.TrimSpace(os.Getenv(envName + "_ARGS")))
		return debugAdapterSpec{Command: command, Args: args, LaunchMode: "generic"}, nil
	}
	switch lang {
	case "go":
		return debugAdapterSpec{Command: "dlv", Args: []string{"dap", "--listen=127.0.0.1:0"}, LaunchMode: "go"}, nil
	case "python":
		return debugAdapterSpec{Command: "python", Args: []string{"-m", "debugpy.adapter"}, LaunchMode: "python"}, nil
	case "javascript", "typescript":
		if command, err := exec.LookPath("js-debug-adapter"); err == nil {
			return debugAdapterSpec{Command: command, Args: []string{"--port", "0"}, LaunchMode: "node"}, nil
		}
		if command, err := exec.LookPath("js-debug"); err == nil {
			return debugAdapterSpec{Command: command, Args: []string{"--port", "0"}, LaunchMode: "node"}, nil
		}
		return debugAdapterSpec{}, fmt.Errorf("no JavaScript/TypeScript DAP adapter found; install js-debug-adapter or configure FUZECLI_DEBUG_ADAPTER_NODE")
	case "rust", "c", "cpp":
		for _, command := range []string{"lldb-dap", "lldb-dap.exe", "gdb"} {
			if path, err := exec.LookPath(command); err == nil {
				if strings.EqualFold(filepath.Base(path), "gdb") {
					return debugAdapterSpec{Command: path, Args: []string{"--interpreter=dap"}, LaunchMode: "native"}, nil
				}
				return debugAdapterSpec{Command: path, Args: []string{"--connection", "listen://127.0.0.1:0"}, LaunchMode: "native"}, nil
			}
		}
		return debugAdapterSpec{}, fmt.Errorf("no native DAP adapter found; install lldb-dap or GDB")
	case "ruby":
		if command, err := exec.LookPath("rdbg"); err == nil {
			return debugAdapterSpec{Command: command, Args: []string{"--open", "--port", "0"}, LaunchMode: "ruby"}, nil
		}
		return debugAdapterSpec{}, fmt.Errorf("Ruby DAP adapter not found; install rdbg")
	case "php":
		if command, err := exec.LookPath("php-debug-adapter"); err == nil {
			return debugAdapterSpec{Command: command, Args: []string{"--port", "0"}, LaunchMode: "php"}, nil
		}
		return debugAdapterSpec{}, fmt.Errorf("PHP DAP adapter not found; install php-debug-adapter")
	case "java", "kotlin":
		return debugAdapterSpec{}, fmt.Errorf("%s debugging requires a JVM DAP adapter; configure FUZECLI_DEBUG_ADAPTER_JVM", lang)
	default:
		return debugAdapterSpec{}, fmt.Errorf("no DAP adapter configured for %s; configure FUZECLI_DEBUG_ADAPTER_%s", language, strings.ToUpper(strings.ReplaceAll(lang, "-", "_")))
	}
}

func startDAP(root, language string) (net.Conn, *exec.Cmd, error) {
	spec, err := debugAdapterForLanguage(language)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	stderr, err := cmd.StderrPipe()
	if err != nil { return nil, nil, err }
	if err := cmd.Start(); err != nil { return nil, nil, fmt.Errorf("start delve: %w", err) }
	ready := make(chan struct{})
	var once sync.Once
	var listenerAddr string
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			fields := strings.Fields(line)
			for i, field := range fields {
				candidate := strings.Trim(strings.TrimSpace(field), ",")
				if strings.HasPrefix(candidate, "127.0.0.1:") || strings.HasPrefix(candidate, "[::1]:") {
					if listenerAddr == "" {
						listenerAddr = candidate
						once.Do(func(){ close(ready) })
					}
					continue
				}
				if i+1 < len(fields) && (field == "127.0.0.1:" || field == "[::1]:") {
					if listenerAddr == "" {
						listenerAddr = strings.Trim(fields[i+1], ",")
						once.Do(func(){ close(ready) })
					}
					continue
				}
			}
		}
		once.Do(func(){ close(ready) })
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return nil, nil, errors.New("Delve did not expose a DAP listener within 5 seconds; install Delve and ensure dlv is on PATH")
	}
	if listenerAddr == "" {
		_ = cmd.Process.Kill()
		return nil, nil, errors.New("Delve exited without exposing a DAP listener; install Delve and ensure dlv is on PATH")
	}
	conn, err := net.DialTimeout("tcp", listenerAddr, 5*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, nil, fmt.Errorf("connect to Delve DAP listener %s: %w", listenerAddr, err)
	}
	return conn, cmd, nil
}

func bridgeDAP(ws *websocket.Conn, root, language string) error {
	conn, cmd, err := startDAP(root, language)
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

func (m *terminalManager) handleWS(ws *websocket.Conn, root string) {
	defer ws.Close()
	s, err := m.start(root)
	if err != nil {
		_ = ws.WriteJSON(map[string]any{"type":"error","message":err.Error()})
		return
	}
	defer m.stop(s.id)
	var writeMu sync.Mutex
	send := func(v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.WriteJSON(v)
	}
	send(map[string]any{"type":"ready","session":s.id,"cwd":root,"shell":runtime.GOOS})
	copyOutput := func(stream string, reader io.Reader) {
		buf := make([]byte, 32<<10)
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				send(map[string]any{"type":"output","stream":stream,"data":string(buf[:n])})
			}
			if readErr != nil {
				return
			}
		}
	}
	go copyOutput("stdout", s.stdout)
	go copyOutput("stderr", s.stderr)
	for {
		var msg struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "input":
			if _, err := io.WriteString(s.stdin, msg.Data); err != nil {
				send(map[string]any{"type":"error","message":"terminal input failed: "+err.Error()})
				return
			}
		case "ping":
			send(map[string]any{"type":"pong"})
		}
	}
}

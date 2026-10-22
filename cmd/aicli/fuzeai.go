package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Enoch7768/fuzecli/internal/config"
)

const (
	fuzeAIModel = "r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M"
	fuzeAIURL   = "http://127.0.0.1:8080"
)

func fuzeAICommand(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printFuzeAIUsage()
		return nil
	}

	switch args[0] {
	case "install":
		if len(args) != 1 {
			return fmt.Errorf("usage: aicli fuzeai install")
		}
		return installFuzeAIRuntime()
	case "setup", "use":
		if len(args) != 1 {
			return fmt.Errorf("usage: aicli fuzeai %s", args[0])
		}
		return configureFuzeAI()
	case "start", "run":
		return startFuzeAI(args[1:])
	case "connect":
		if len(args) != 2 {
			return fmt.Errorf("usage: aicli fuzeai connect https://YOUR-TUNNEL.trycloudflare.com")
		}
		return connectFuzeAI(args[1])
	case "disconnect":
		if len(args) != 1 {
			return fmt.Errorf("usage: aicli fuzeai disconnect")
		}
		return disconnectFuzeAI()
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: aicli fuzeai status")
		}
		return fuzeAIStatus()
	default:
		return fmt.Errorf("unknown FuzeAI command %q; run 'aicli fuzeai help'", args[0])
	}
}

func installFuzeAIRuntime() error {
	if _, err := exec.LookPath("llama-server"); err == nil {
		fmt.Println("llama-server is already installed.")
		return nil
	}
	var command string
	var args []string
	switch runtime.GOOS {
	case "windows":
		command = "winget"
		args = []string{"install", "llama.cpp"}
	case "darwin", "linux":
		command = "brew"
		args = []string{"install", "llama.cpp"}
	default:
		return fmt.Errorf("automatic llama.cpp installation is not supported on %s; see https://github.com/ggml-org/llama.cpp/releases", runtime.GOOS)
	}
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Errorf("%s is required to install llama.cpp; install it first or use https://github.com/ggml-org/llama.cpp/releases", command)
	}
	fmt.Printf("Installing llama.cpp with %s %s\n", command, strings.Join(args, " "))
	cmd := exec.Command(command, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("llama.cpp installation failed: %w", err)
	}
	fmt.Println("Installation command completed. Open a new terminal so PATH updates are loaded, then run 'aicli fuzeai start'.")
	return nil
}

func configureFuzeAI() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	local := cfg.Providers["llamacpp"]
	if local.BaseURL == "" {
		local.BaseURL = fuzeAIURL
	}
	local.DefaultModel = fuzeAIModel
	cfg.Providers["llamacpp"] = local
	cfg.DefaultProvider = "llamacpp"
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Println("FuzeAI is now FuzeCLI's default model.")
	fmt.Printf("  Model:    %s\n", local.DefaultModel)
	fmt.Printf("  Endpoint: %s/v1\n", strings.TrimRight(local.BaseURL, "/"))
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  1. Run 'aicli fuzeai start' to download the GGUF on first launch and start the local server.")
	fmt.Println("  2. Open another terminal in your project and run 'aicli chat'.")
	return nil
}

func startFuzeAI(args []string) error {
	port := 8080
	contextSize := 4096
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --port")
			}
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value < 1 || value > 65535 {
				return fmt.Errorf("invalid port %q", args[i+1])
			}
			port = value
			i++
		case "--context":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for --context")
			}
			value, err := strconv.Atoi(args[i+1])
			if err != nil || value < 512 || value > 131072 {
				return fmt.Errorf("context must be between 512 and 131072 tokens")
			}
			contextSize = value
			i++
		case "--help", "-h":
			fmt.Println("Usage: aicli fuzeai start [--port 8080] [--context 4096]")
			fmt.Println("The server downloads the GGUF from Hugging Face on first launch.")
			return nil
		default:
			return fmt.Errorf("unknown fuzeai start option %q", args[i])
		}
	}

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	local := cfg.Providers["llamacpp"]
	if local.DefaultModel == "" || local.DefaultModel == "local" {
		local.DefaultModel = fuzeAIModel
	}
	local.BaseURL = endpoint
	cfg.Providers["llamacpp"] = local
	if err := config.Save(cfg); err != nil {
		return err
	}
	if running, err := serverResponds(endpoint); err == nil && running {
		fmt.Printf("FuzeAI is already responding at %s/v1.\n", endpoint)
		return nil
	}

	server, err := exec.LookPath("llama-server")
	if err != nil {
		if alt, altErr := exec.LookPath("llama-server.exe"); altErr == nil {
			server = alt
		} else {
			return fmt.Errorf("llama-server was not found. Install llama.cpp from https://github.com/ggml-org/llama.cpp/releases, then retry. FuzeCLI does not install system software without your permission")
		}
	}

	fmt.Printf("Starting FuzeAI from Hugging Face: %s\n", local.DefaultModel)
	fmt.Println("The GGUF is downloaded automatically by llama.cpp if it is not cached yet.")
	fmt.Printf("Server: %s (context: %d)\n", endpoint, contextSize)
	fmt.Println("Press Ctrl+C to stop the model server. Keep this terminal open; use another terminal for 'aicli chat'.")
	cmd := exec.Command(server,
		"-hf", local.DefaultModel,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-c", strconv.Itoa(contextSize),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("FuzeAI model server stopped: %w", err)
	}
	return nil
}

func connectFuzeAI(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("endpoint must be a clean HTTPS URL, for example https://your-name.trycloudflare.com")
	}
	endpoint := strings.TrimRight(parsed.String(), "/")
	if !isPublicHTTPSHost(parsed.Hostname()) {
		return fmt.Errorf("endpoint must use a public HTTPS hostname")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	local := cfg.Providers["llamacpp"]
	if local.DefaultModel == "" || local.DefaultModel == "local" {
		local.DefaultModel = fuzeAIModel
	}
	local.BaseURL = endpoint
	cfg.Providers["llamacpp"] = local
	cfg.DefaultProvider = "llamacpp"
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Println("FuzeAI remote endpoint configured.")
	fmt.Printf("  Model:    %s\n  Endpoint: %s/v1\n", local.DefaultModel, endpoint)
	fmt.Println("Run 'aicli fuzeai status' to test it, then 'aicli chat' in your project.")
	fmt.Println("No model API key is configured. Keep this tunnel URL private: anyone who obtains it may use the model.")
	return nil
}

func disconnectFuzeAI() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	local := cfg.Providers["llamacpp"]
	local.BaseURL = fuzeAIURL
	if local.DefaultModel == "" {
		local.DefaultModel = fuzeAIModel
	}
	cfg.Providers["llamacpp"] = local
	cfg.DefaultProvider = "llamacpp"
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Println("FuzeAI endpoint reset to local mode at http://127.0.0.1:8080.")
	fmt.Println("Run 'aicli fuzeai start' to use the model on this computer.")
	return nil
}

func isPublicHTTPSHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified()
	}
	return strings.Contains(host, ".")
}

func remoteServerResponds(endpoint string) (bool, error) {
	parsed, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || !isPublicHTTPSHost(parsed.Hostname()) {
		return false, fmt.Errorf("remote FuzeAI endpoint must be a public HTTPS URL")
	}
	modelsURL := strings.TrimRight(endpoint, "/")
	if !strings.HasSuffix(modelsURL, "/v1") {
		modelsURL += "/v1"
	}
	modelsURL += "/models"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return false, err
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

func fuzeAIStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	local := cfg.Providers["llamacpp"]
	endpoint := strings.TrimRight(local.BaseURL, "/")
	if endpoint == "" {
		endpoint = fuzeAIURL
	}
	fmt.Println("FuzeAI integration")
	fmt.Printf("  Model:           %s\n", local.DefaultModel)
	fmt.Printf("  Endpoint:        %s/v1\n", endpoint)
	fmt.Printf("  Default provider: %s\n", cfg.DefaultProvider)

	var running bool
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil && parsed.Scheme == "https" && !isLocalHost(parsed.Hostname()) {
		running, err = remoteServerResponds(endpoint)
	} else {
		running, err = serverResponds(endpoint)
	}
	if err != nil {
		return fmt.Errorf("cannot check FuzeAI server: %w", err)
	}
	if !running {
		fmt.Println("  Status:          stopped or not responding")
		if strings.HasPrefix(endpoint, "https://") {\n\t\t\tfmt.Println("  Next step:       Restart the Kaggle notebook and reconnect with a new tunnel URL")\n\t\t} else {\n\t\t\tfmt.Println("  Start it with:   aicli fuzeai start")\n\t\t}
		return nil
	}
	fmt.Println("  Status:          responding")
	return nil
}

func serverResponds(endpoint string) (bool, error) {
	parsed, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false, fmt.Errorf("invalid local model endpoint %q", endpoint)
	}
	host := parsed.Hostname()
	if !isLocalHost(host) {
		return false, fmt.Errorf("refusing to probe non-local model endpoint %q", parsed.Host)
	}
	modelsURL := strings.TrimRight(endpoint, "/")
	if !strings.HasSuffix(modelsURL, "/v1") {
		modelsURL += "/v1"
	}
	modelsURL += "/models"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func printFuzeAIUsage() {
	fmt.Println(`FuzeAI - the coding model integrated with FuzeCLI

Usage:
  aicli fuzeai install               Install the llama.cpp runtime
  aicli fuzeai setup                 Configure local FuzeAI
  aicli fuzeai start [options]       Start the local model server
  aicli fuzeai connect <https-url>   Connect to a Kaggle/tunnel endpoint
  aicli fuzeai disconnect            Reset endpoint to local mode
  aicli fuzeai status                Check the configured model server
  aicli fuzeai help                  Show these commands

Start options:
  --port <port>                     Local server port (default 8080)
  --context <tokens>                Context size (default 4096)

Local setup:
  aicli fuzeai setup
  aicli fuzeai start
  # In a second terminal:
  aicli chat

Remote Kaggle endpoint:
  aicli fuzeai connect https://YOUR-TUNNEL.trycloudflare.com
  aicli fuzeai status
  aicli fuzeai disconnect

Model: r7194714R/fuzeai-qwen-7b-gguf:Q4_K_M
The GGUF is downloaded from Hugging Face by llama.cpp.
See docs/FUZEAi_INTEGRATION.md for the Kaggle GPU instructions.`)
}

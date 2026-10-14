package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type installRuntimeRequest struct {
	Runtime string `json:"runtime"`
}

type runtimePackage struct {
	Winget string
	Brew string
	APT string
	DNF string
	Pacman string
}

var runtimePackages = map[string]runtimePackage{
	"node": {"OpenJS.NodeJS.LTS", "node", "nodejs", "nodejs", "nodejs"},
	"node.js": {"OpenJS.NodeJS.LTS", "node", "nodejs", "nodejs", "nodejs"},
	"python": {"Python.Python.3.12", "python", "python3", "python3", "python"},
	"python3": {"Python.Python.3.12", "python", "python3", "python3", "python"},
	"php": {"PHP.PHP.8.4", "php", "php-cli", "php-cli", "php"},
	"go": {"GoLang.Go", "go", "golang-go", "golang", "go"},
	"rust": {"Rustlang.Rustup", "rust", "rustc", "rust", "rust"},
	"rustc": {"Rustlang.Rustup", "rust", "rustc", "rust", "rust"},
	"rust (rustc)": {"Rustlang.Rustup", "rust", "rustc", "rust", "rust"},
	"node.js + tsx": {"OpenJS.NodeJS.LTS", "node", "nodejs", "nodejs", "nodejs"},
	"c/c++ compiler": {"MSYS2.MSYS2", "gcc", "build-essential", "gcc-c++", "base-devel"},
	"java": {"EclipseAdoptium.Temurin.21.JDK", "openjdk", "default-jdk", "java-21-openjdk-devel", "jdk-openjdk"},
	"kotlinc": {"JetBrains.Kotlin.Compiler", "kotlin", "kotlin", "kotlin", "kotlin"},
	"kotlin compiler + java": {"JetBrains.Kotlin.Compiler", "kotlin", "kotlin", "kotlin", "kotlin"},
	"dotnet": {"Microsoft.DotNet.SDK.8", "dotnet-sdk", "dotnet-sdk-8.0", "dotnet-sdk-8.0", "dotnet-sdk"},
	"ruby": {"RubyInstallerTeam.RubyWithDevKit.3.3", "ruby", "ruby-full", "ruby", "ruby"},
	"perl": {"StrawberryPerl.StrawberryPerl", "perl", "perl", "perl", "perl"},
	"lua": {"DEVCOM.Lua", "lua", "lua5.4", "lua", "lua"},
	"rscript": {"RProject.R", "r", "r-base", "R", "r"},
	"julia": {"Julialang.Julia", "julia", "julia", "julia", "julia"},
	"swift": {"Swift.Toolchain", "swift", "", "", ""},
	"dart": {"Google.DartSDK", "dart", "dart", "dart", "dart"},
	"elixir": {"Elixir.Elixir", "elixir", "elixir", "elixir", "elixir"},
	"runghc": {"Haskell.GHC", "ghc", "ghc", "ghc", "ghc"},
	"scala": {"Scala.Scala", "scala", "scala", "scala", "scala"},
	"powershell": {"Microsoft.PowerShell", "powershell", "powershell", "powershell", "powershell"},
	"bash": {"Git.Git", "bash", "bash", "bash", "bash"},
	"fortran compiler": {"", "gcc", "gfortran", "gcc-gfortran", "gcc-fortran"},
	"pascal compiler": {"", "fpc", "fpc", "fpc", "fpc"},
	"d compiler": {"Dlang.DMD", "dmd", "gdc", "gdc", "dmd"},
	"dmd": {"Dlang.DMD", "dmd", "gdc", "gdc", "dmd"},
	"zig": {"", "zig", "zig", "zig", "zig"},
	"nim": {"", "nim", "nim", "nim", "nim"},
	"clojure": {"", "clojure", "clojure", "clojure", "clojure"},
	"groovy": {"", "groovy", "groovy", "groovy", "groovy"},
	"crystal": {"", "crystal", "crystal", "crystal", "crystal"},
	"racket": {"", "racket", "racket", "racket", "racket"},
}

func (s *Server) installRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var req installRuntimeRequest
	if err := jsonNewDecoder(r).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime installation request")
		return
	}
	key := strings.ToLower(strings.TrimSpace(req.Runtime))
	pkg, ok := runtimePackages[key]
	if !ok {
		writeError(w, http.StatusBadRequest, "automatic installation is not configured for this runtime; use the official installer")
		return
	}
	command, args, err := installCommand(pkg)
	if key == "node.js + tsx" {
		npm := "npm"
		if runtime.GOOS == "windows" {
			if _, lookupErr := exec.LookPath("npm"); lookupErr != nil {
				err = fmt.Errorf("Node.js/npm is required before installing the TypeScript runner")
			} else {
				command, args, err = "cmd.exe", []string{"/d", "/s", "/c", "npm install --global tsx"}, nil
			}
		} else if _, lookupErr := exec.LookPath(npm); lookupErr != nil {
			err = fmt.Errorf("Node.js/npm is required before installing the TypeScript runner")
		} else {
			command, args, err = npm, []string{"install", "--global", "tsx"}, nil
		}
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = os.Environ()
	out := &boundedBuffer{limit: 128 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		message := err.Error()
		if ctx.Err() == context.DeadlineExceeded { message = "Runtime installation timed out." }
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": message, "output": out.String()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runtime": key, "output": out.String(), "message": "Installation completed. Restart the workspace if the runtime is not yet on PATH."})
}

func installCommand(pkg runtimePackage) (string, []string, error) {
	switch runtime.GOOS {
	case "windows":
		if _, err := exec.LookPath("winget"); err == nil && pkg.Winget != "" {
			return "winget", []string{"install", "--id", pkg.Winget, "--exact", "--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}, nil
		}
		if _, err := exec.LookPath("choco"); err == nil && pkg.Brew != "" {
			return "choco", []string{"install", strings.ToLower(pkg.Brew), "-y", "--no-progress"}, nil
		}
		return "", nil, fmt.Errorf("no supported package manager found; install Winget or use the official runtime installer")
	case "darwin":
		if _, err := exec.LookPath("brew"); err != nil { return "", nil, fmt.Errorf("Homebrew is required for automatic installation on macOS") }
		if pkg.Brew == "" { return "", nil, fmt.Errorf("automatic installation is not configured for this runtime on macOS") }
		return "brew", []string{"install", pkg.Brew}, nil
	default:
		if _, err := exec.LookPath("apt-get"); err == nil {
			if pkg.APT == "" { return "", nil, fmt.Errorf("automatic installation is not configured for this runtime on Debian/Ubuntu") }
			if _, err := exec.LookPath("sudo"); err != nil { return "", nil, fmt.Errorf("sudo is required; install the runtime using your system package manager") }
			return "sudo", []string{"-n", "apt-get", "install", "-y", pkg.APT}, nil
		}
		if _, err := exec.LookPath("dnf"); err == nil && pkg.DNF != "" { return "sudo", []string{"-n", "dnf", "install", "-y", pkg.DNF}, nil }
		if _, err := exec.LookPath("pacman"); err == nil && pkg.Pacman != "" { return "sudo", []string{"-n", "pacman", "-S", "--noconfirm", pkg.Pacman}, nil }
		return "", nil, fmt.Errorf("no supported package manager found; install the runtime using your distribution's package manager")
	}
}

func jsonNewDecoder(r *http.Request) *json.Decoder {
	return json.NewDecoder(r.Body)
}

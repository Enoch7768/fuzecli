package api

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"runtime"
	"testing"
)

func TestNodePreviewUsesRecognizedServerEntryWithoutPackageJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "server.js"), []byte("console.log('server')"), 0600); err != nil {
		t.Fatal(err)
	}
	command, args, env, err := runtimeCommand(root, "node", 43123)
	if err != nil {
		t.Fatal(err)
	}
	if command != "node" || !reflect.DeepEqual(args, []string{"server.js"}) {
		t.Fatalf("command/args = %q %v, want node [server.js]", command, args)
	}
	if !containsEnv(env, "PORT=43123") {
		t.Fatalf("PORT was not supplied: %v", env)
	}
}

func TestNodePreviewDoesNotPassViteFlagsToGenericStartScript(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"scripts":{"start":"node server.js"}}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	command, args, _, err := runtimeCommand(root, "node", 43124)
	if err != nil {
		t.Fatal(err)
	}
	wantCommand, wantArgs := "npm", []string{"run", "start"}
	if runtime.GOOS == "windows" { wantCommand, wantArgs = "cmd.exe", []string{"/d", "/s", "/c", "npm.cmd run start"} }
	if command != wantCommand || !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("command/args = %q %v, want %q %v without framework flags", command, args, wantCommand, wantArgs)
	}
}

func TestNodePreviewAddsHostFlagsForVite(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"scripts":{"dev":"vite"}}`)
	if err := os.WriteFile(filepath.Join(root, "package.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	command, args, _, err := runtimeCommand(root, "node", 43125)
	if err != nil {
		t.Fatal(err)
	}
	wantCommand, wantArgs := "npm", []string{"run", "dev", "--", "--host", "127.0.0.1", "--port", "43125"}
	if runtime.GOOS == "windows" { wantCommand, wantArgs = "cmd.exe", []string{"/d", "/s", "/c", "npm.cmd run dev -- --host 127.0.0.1 --port 43125"} }
	if command != wantCommand || !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("command/args = %q %v, want Vite host and port flags %q %v", command, args, wantCommand, wantArgs)
	}
}

func TestPythonPreviewUsesStaticServerForPlainScript(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("print('hello')"), 0600); err != nil {
		t.Fatal(err)
	}
	command, args, _, err := runtimeCommand(root, "python", 43126)
	if err != nil {
		t.Fatal(err)
	}
	if command != pythonExecutable() || !reflect.DeepEqual(args, []string{"-m", "http.server", "43126", "--bind", "127.0.0.1"}) {
		t.Fatalf("command/args = %q %v, want Python static server", command, args)
	}
}

func TestPythonPreviewDetectsFlaskFromSource(t *testing.T) {
	root := t.TempDir()
	source := "from flask import Flask\napp = Flask(__name__)\napp.run()"
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte(source), 0600); err != nil { t.Fatal(err) }
	command, args, _, err := runtimeCommand(root, "python", 43129)
	if err != nil { t.Fatal(err) }
	want := []string{"-m", "flask", "--app", "app.py", "run", "--host", "127.0.0.1", "--port", "43129"}
	if command != pythonExecutable() || !reflect.DeepEqual(args, want) { t.Fatalf("command/args = %q %v, want Flask runtime", command, args) }
}

func TestPythonPreviewDetectsFastAPIFromSource(t *testing.T) {
	root := t.TempDir()
	source := "from fastapi import FastAPI\napp = FastAPI()\nuvicorn.run(app)"
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(source), 0600); err != nil { t.Fatal(err) }
	command, args, _, err := runtimeCommand(root, "python", 43130)
	if err != nil { t.Fatal(err) }
	want := []string{"-m", "uvicorn", "main:app", "--host", "127.0.0.1", "--port", "43130"}
	if command != pythonExecutable() || !reflect.DeepEqual(args, want) { t.Fatalf("command/args = %q %v, want FastAPI runtime", command, args) }
}

func TestPHPPreviewUsesPublicIndexWhenPresent(t *testing.T) {
	root := t.TempDir()
	publicRoot := filepath.Join(root, "public")
	if err := os.MkdirAll(publicRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicRoot, "index.php"), []byte("<?php echo 'ok';"), 0600); err != nil {
		t.Fatal(err)
	}
	command, args, _, err := runtimeCommand(root, "php", 43127)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-S", "127.0.0.1:43127", "-t", publicRoot, filepath.Join(publicRoot, "index.php")}
	if command != "php" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command/args = %q %v, want PHP public-root router", command, args)
	}
}

func containsEnv(env []string, expected string) bool {
	for _, value := range env {
		if value == expected {
			return true
		}
	}
	return false
}

func TestRuntimePreviewUsesPlatformSpecificNPMName(t *testing.T) {
	if runtime.GOOS == "windows" {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0600); err != nil {
			t.Fatal(err)
		}
		command, _, _, err := runtimeCommand(root, "node", 43128)
		if err != nil {
			t.Fatal(err)
		}
		if command != "cmd.exe" {
			t.Fatalf("command = %q, want cmd.exe wrapper on Windows", command)
		}
	}
}

func TestNodePreviewRespectsPackageManagerAndLockfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		manifest string
		lock string
		wantManager string
	}{
		{name: "declared pnpm", manifest: `{"packageManager":"pnpm@9.0.0","scripts":{"dev":"vite"}}`, lock: "pnpm-lock.yaml", wantManager: "pnpm"},
		{name: "declared yarn", manifest: `{"packageManager":"yarn@4.0.0","scripts":{"dev":"vite"}}`, lock: "yarn.lock", wantManager: "yarn"},
		{name: "bun lock", manifest: `{"scripts":{"start":"node server.js"}}`, lock: "bun.lock", wantManager: "bun"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(tc.manifest), 0600); err != nil { t.Fatal(err) }
			if err := os.WriteFile(filepath.Join(root, tc.lock), []byte(""), 0600); err != nil { t.Fatal(err) }
			command, args, _, err := runtimeCommand(root, "node", 43131)
			if err != nil { t.Fatal(err) }
			wantArgs := []string{"run", "dev", "--", "--host", "127.0.0.1", "--port", "43131"}
			if tc.wantManager == "bun" { wantArgs = []string{"run", "start"} }
			wantCommand := tc.wantManager
			if runtime.GOOS == "windows" {
				wantCommand = "cmd.exe"
				wantArgs = []string{"/d", "/s", "/c", tc.wantManager + ".cmd " + strings.Join(wantArgs, " ")}
			}
			if command != wantCommand || !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("command/args = %q %v, want %q %v", command, args, wantCommand, wantArgs)
			}
		})
	}
}

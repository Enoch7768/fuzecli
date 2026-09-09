package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/Enoch7768/fuzecli/internal/app"
	"github.com/Enoch7768/fuzecli/internal/progress"
	"github.com/Enoch7768/fuzecli/internal/web"
)

func webCommand() error {
	a, err := app.Load()
	if err != nil {
		return err
	}
	defer a.Close()

	if err := a.AttachWorkspace("."); err != nil {
		return err
	}

	hub := progress.New()
	server := web.New(a, hub)

	url := "http://" + server.Addr
	fmt.Println("FuzeCLI web UI")
	fmt.Println("Workspace:", a.Store.Root)
	fmt.Println("Listening:", url)
	fmt.Println("Press Ctrl+C to stop.")

	openBrowser(url)
	return server.ListenAndServe(context.Background())
}

func openBrowser(url string) {
	var command string
	var args []string

	switch runtime.GOOS {
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		command = "open"
		args = []string{url}
	default:
		command = "xdg-open"
		args = []string{url}
	}

	_ = exec.Command(command, args...).Start()
}

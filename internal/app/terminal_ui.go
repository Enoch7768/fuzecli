package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	uiReset   = "\x1b[0m"
	uiBold    = "\x1b[1m"
	uiMuted   = "\x1b[38;5;244m"
	uiAccent  = "\x1b[38;5;111m"
	uiAccent2 = "\x1b[38;5;117m"
	uiLine    = "\x1b[38;5;239m"
	uiGreen   = "\x1b[38;5;114m"
)

func printTerminalWorkspace(providerName, model, root string) {
	width := terminalWidth()
	if width < 88 {
		printCompactTerminalWorkspace(providerName, model, root)
		return
	}

	name := filepath.Base(root)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "Workspace"
	}

	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("%s%sFUZECLI%s  %sLOCAL CODING WORKSPACE%s\n", uiBold, uiAccent2, uiReset, uiMuted, uiReset)
	fmt.Printf("%s%s%s\n", uiLine, strings.Repeat("─", width), uiReset)

	sidebar := []string{
		"Overview",
		"Files",
		"Assistant",
		"Terminal",
		"Workspace",
		"Live Preview",
		"Settings",
	}

	navWidth := 21
	mainWidth := width - navWidth - 5
	if mainWidth < 54 {
		mainWidth = 54
	}

	top := fmt.Sprintf("%s%s  %s%s", uiBold, uiAccent2, name, uiReset)
	fmt.Printf("│ %-19s │ %s%-*s%s │\n", " ", top, mainWidth-2, "", uiReset)
	fmt.Printf("│ %s%-19s%s │ %s%s%s\n", uiMuted, "NAVIGATION", uiReset, uiMuted, strings.Repeat("─", mainWidth), uiReset)

	for i, item := range sidebar {
		prefix := "  "
		style := uiMuted
		if i == 0 {
			prefix = "▸ "
			style = uiAccent
		}
		fmt.Printf("│ %s%s%-17s%s │ ", style, prefix, item, uiReset)
		switch i {
		case 0:
			fmt.Printf("%s%s%s\n", uiBold, "Workspace at a glance", uiReset)
		case 1:
			fmt.Printf("%sBrowse and attach project files%s\n", uiMuted, uiReset)
		case 2:
			fmt.Printf("%sChat, plan, generate and repair%s\n", uiMuted, uiReset)
		case 3:
			fmt.Printf("%sRun your local terminal workflow%s\n", uiMuted, uiReset)
		case 4:
			fmt.Printf("%sActive project and session state%s\n", uiMuted, uiReset)
		case 5:
			fmt.Printf("%sPreview the current project%s\n", uiMuted, uiReset)
		case 6:
			fmt.Printf("%sProviders, models and preferences%s\n", uiMuted, uiReset)
		}
	}

	fmt.Printf("│ %s%-19s%s │ %s%s%s\n", uiLine, "", uiReset, uiLine, strings.Repeat("─", mainWidth), uiReset)
	fmt.Printf("│ %-19s │ %s%sBuild. Inspect. Improve.%s\n", " ", uiBold, uiAccent2, uiReset)
	fmt.Printf("│ %-19s │ %sTurn your workspace into a focused coding session.%s\n", " ", uiMuted, uiReset)
	fmt.Printf("│ %-19s │\n", " ")

	card1 := fmt.Sprintf("  %s%sFILES%s\n  %sProject files available to the assistant.%s", uiBold, uiAccent, uiReset, uiMuted, uiReset)
	card2 := fmt.Sprintf("  %s%sASSISTANT%s\n  %sProvider: %s%s%s  ·  Model: %s%s%s", uiBold, uiAccent, uiReset, uiMuted, uiAccent2, providerName, uiReset, uiAccent2, model, uiReset)
	card3 := fmt.Sprintf("  %s%sWORKSPACE%s\n  %s%s%s", uiBold, uiAccent, uiReset, uiMuted, root, uiReset)

	printTerminalCard(mainWidth, card1)
	printTerminalCard(mainWidth, card2)
	printTerminalCard(mainWidth, card3)

	fmt.Printf("│ %-19s │ %s%s%s\n", " ", uiMuted, "Ready for your next request.", uiReset)
	fmt.Printf("│ %-19s │\n", " ")
	fmt.Printf("│ %-19s │ %s%s%s\n", " ", uiLine, strings.Repeat("─", mainWidth), uiReset)
	fmt.Printf("│ %s%s%s │ %s%s%s\n", uiMuted, "  SESSION", uiReset, uiMuted, " /file  /provider  /model  /status  /help  /exit", uiReset)
	fmt.Printf("│ %-19s │ %s%s%s\n", " ", uiGreen, "●", uiReset)
	fmt.Printf("%s%s%s\n", uiLine, strings.Repeat("─", width), uiReset)
	fmt.Printf("%s❯%s ", uiAccent, uiReset)
}

func printTerminalCard(width int, body string) {
	lines := strings.Split(body, "\n")
	inner := width - 4
	if inner < 20 {
		inner = 20
	}
	fmt.Printf("│ %-19s │ %s┌%s┐%s\n", " ", uiLine, strings.Repeat("─", inner), uiReset)
	for _, line := range lines {
		plain := stripANSI(line)
		padding := inner - 2 - len([]rune(plain))
		if padding < 0 {
			padding = 0
		}
		fmt.Printf("│ %-19s │ %s│%s%s%s  %s│%s\n", " ", uiLine, uiReset, line, "", strings.Repeat(" ", padding), uiLine, uiReset)
	}
	fmt.Printf("│ %-19s │ %s└%s┘%s\n", " ", uiLine, strings.Repeat("─", inner), uiReset)
}

func printCompactTerminalWorkspace(providerName, model, root string) {
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("%s%sFUZECLI%s  %s%s%s\n", uiBold, uiAccent2, uiReset, uiMuted, filepath.Base(root), uiReset)
	fmt.Printf("%s%s%s\n", uiLine, strings.Repeat("─", 64), uiReset)
	fmt.Printf("%s▸ Overview%s\n", uiAccent, uiReset)
	fmt.Println("  Files")
	fmt.Println("  Assistant")
	fmt.Println("  Terminal")
	fmt.Println("  Workspace")
	fmt.Println("  Live Preview")
	fmt.Println("  Settings")
	fmt.Printf("\n%sBuild. Inspect. Improve.%s\n", uiBold, uiReset)
	fmt.Printf("%sProvider:%s %s  %sModel:%s %s\n", uiMuted, uiReset, providerName, uiMuted, uiReset, model)
	fmt.Printf("%sWorkspace:%s %s\n\n", uiMuted, uiReset, root)
	fmt.Printf("%s❯%s ", uiAccent, uiReset)
}

func terminalWidth() int {
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		var width int
		if _, err := fmt.Sscanf(value, "%d", &width); err == nil && width >= 64 {
			if width > 140 {
				return 140
			}
			return width
		}
	}
	return 104
}

func stripANSI(s string) string {
	for {
		start := strings.IndexByte(s, '\x1b')
		if start < 0 {
			return s
		}
		end := start + 1
		for end < len(s) && s[end] != 'm' && s[end] != 'K' {
			end++
		}
		if end < len(s) {
			end++
		}
		s = s[:start] + s[end:]
	}
}

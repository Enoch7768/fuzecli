package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enoch7768/fuzecli/internal/security"
)

func TestTier3ToolDefinitionsAreStrictAndRegistered(t *testing.T) {
	defs := FuzeCLIToolDefinitions()
	if len(defs) != 91 {
		t.Fatalf("want 91 total tools across Tiers 1-4, got %d", len(defs))
	}
	want := []string{
		"image.inspect", "image.compare", "browser.open", "browser.click", "browser.type", "browser.select",
		"browser.screenshot", "browser.inspect", "browser.console", "browser.network", "deploy.inspect", "deploy.run",
	}
	seen := map[string]bool{}
	for _, def := range defs {
		if seen[def.Name] {
			t.Errorf("duplicate tool name %q", def.Name)
		}
		seen[def.Name] = true
		if !json.Valid(def.InputSchema) {
			t.Errorf("%s has invalid JSON schema", def.Name)
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Errorf("%s schema cannot be decoded: %v", def.Name, err)
			continue
		}
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s schema must be a strict object", def.Name)
		}
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("missing Tier 3 tool %q", name)
		}
	}
	if _, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: t.TempDir()}); err != nil {
		t.Fatalf("registry could not register all tools: %v", err)
	}
}

func TestTier3ImageInspectAndPixelComparison(t *testing.T) {
	root := t.TempDir()
	writePNG := func(name string, fill color.Color) {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				img.Set(x, y, fill)
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), buf.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writePNG("a.png", color.RGBA{R: 255, A: 255})
	writePNG("b.png", color.RGBA{R: 255, A: 255})
	writePNG("c.png", color.RGBA{B: 255, A: 255})

	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	inspect := registry.Execute(context.Background(), ToolCall{ID: "inspect", Name: "image.inspect", Arguments: json.RawMessage(`{"path":"a.png"}`)}, nil)
	if inspect.Status != ToolResultSuccess {
		t.Fatalf("image.inspect failed: %s", inspect.Error)
	}
	var metadata map[string]any
	if err := json.Unmarshal(inspect.Result, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["width"] != float64(8) || metadata["height"] != float64(8) || metadata["format"] != "png" {
		t.Fatalf("unexpected image metadata: %#v", metadata)
	}

	compare := func(a, b string) map[string]any {
		t.Helper()
		result := registry.Execute(context.Background(), ToolCall{ID: "compare", Name: "image.compare", Arguments: json.RawMessage([]byte(`{"path_a":` + tier3QuoteJSON(a) + `,"path_b":` + tier3QuoteJSON(b) + `}`))}, nil)
		if result.Status != ToolResultSuccess {
			t.Fatalf("image.compare failed: %s", result.Error)
		}
		var value map[string]any
		if err := json.Unmarshal(result.Result, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	same := compare("a.png", "b.png")
	if same["exact_sample_match_ratio"] != float64(1) {
		t.Fatalf("identical images should have exact match ratio 1: %#v", same)
	}
	different := compare("a.png", "c.png")
	if different["exact_sample_match_ratio"] != float64(0) {
		t.Fatalf("different images should have exact match ratio 0: %#v", different)
	}
}

func tier3QuoteJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestTier3BrowserSafetyAndApprovalPolicies(t *testing.T) {
	if _, err := tier3BrowserURL("javascript:alert(1)"); err == nil {
		t.Fatal("javascript: URL must be rejected")
	}
	if _, err := tier3BrowserURL("https://user:secret@example.com"); err == nil {
		t.Fatal("URLs with embedded credentials must be rejected")
	}
	t.Setenv("FUZECLI_BROWSER_CDP_URL", "http://example.com:9222")
	if _, err := tier3CDPBase(); err == nil {
		t.Fatal("remote DevTools endpoint must be rejected")
	}
	if tier3LoopbackWebSocket("ws://example.com/devtools/page/1") { t.Fatal("remote WebSocket debugger target must be rejected") }
	if !tier3LoopbackWebSocket("ws://127.0.0.1:9222/devtools/page/1") { t.Fatal("loopback WebSocket debugger target should be accepted") }
	for _, item := range []struct{name, args string}{
		{"browser.click", `{"target_id":"x","selector":"button"}`},
		{"browser.type", `{"target_id":"x","selector":"input","text":"hello"}`},
		{"browser.select", `{"target_id":"x","selector":"select","value":"x"}`},
		{"deploy.run", `{"provider":"vercel"}`},
		{"browser.screenshot", `{"target_id":"x","path":"screens/page.png"}`},
	} {
		required, _ := RequiresToolApproval(item.name, json.RawMessage(item.args))
		if !required {
			t.Errorf("%s must require explicit approval", item.name)
		}
	}
	if required, _ := RequiresToolApproval("browser.screenshot", json.RawMessage(`{"target_id":"x"}`)); required {
		t.Fatal("inline screenshot must not require workspace-write approval")
	}
}

func TestTier3DeploymentInspectionAndDryRunNeverDeploy(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "vercel.json"), []byte(`{"version":2}`), 0644); err != nil {
		t.Fatal(err)
	}
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	inspect := registry.Execute(context.Background(), ToolCall{ID: "inspect", Name: "deploy.inspect", Arguments: json.RawMessage(`{"provider":"auto"}`)}, nil)
	if inspect.Status != ToolResultSuccess {
		t.Fatalf("deploy.inspect failed: %s", inspect.Error)
	}
	if !strings.Contains(string(inspect.Result), "vercel.json") {
		t.Fatalf("deployment manifest not reported: %s", inspect.Result)
	}
	dry := registry.Execute(context.Background(), ToolCall{ID: "dry", Name: "deploy.run", Arguments: json.RawMessage(`{"provider":"vercel","production":true,"dry_run":true}`)}, nil)
	if dry.Status != ToolResultSuccess {
		t.Fatalf("deployment dry run failed: %s", dry.Error)
	}
	if strings.Contains(string(dry.Result), `"executed":true`) {
		t.Fatalf("dry run claims a deployment was executed: %s", dry.Result)
	}
	if !strings.Contains(string(dry.Result), "--prod") {
		t.Fatalf("production dry-run command must show production flag: %s", dry.Result)
	}
}

func TestTier3BrowserOpenRejectsUnsafeURLBeforeContactingCDP(t *testing.T) {
	caps := security.SafeCapabilities()
	caps[security.NetworkAccess] = true
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: t.TempDir(), Capabilities: caps})
	if err != nil {
		t.Fatal(err)
	}
	result := registry.Execute(context.Background(), ToolCall{ID: "open", Name: "browser.open", Arguments: json.RawMessage(`{"url":"javascript:alert(1)"}`)}, nil)
	if result.Status != ToolResultError || !strings.Contains(strings.ToLower(result.Error), "http(s)") {
		t.Fatalf("unsafe browser URL was not rejected: %#v", result)
	}
}

func TestTier3FlyProductionTargetFailsClosed(t *testing.T) {
	registry, err := NewFuzeCLIToolRegistry(ToolRuntime{Root: t.TempDir()})
	if err != nil { t.Fatal(err) }
	result := registry.Execute(context.Background(), ToolCall{ID: "fly-prod", Name: "deploy.run", Arguments: json.RawMessage(`{"provider":"fly","production":true,"dry_run":true}`)}, nil)
	if result.Status != ToolResultError || !strings.Contains(strings.ToLower(result.Error), "fly.toml") {
		t.Fatalf("Fly production target should not be guessed: %#v", result)
	}
}

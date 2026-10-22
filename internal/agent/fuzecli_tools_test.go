package agent

import (
 "context"
 "encoding/json"
 "strings"
 "testing"
)

func TestFuzeCLIToolDefinitions(t *testing.T) {
 defs := FuzeCLIToolDefinitions()
 if len(defs) != 20 { t.Fatalf("want 20 tools, got %d", len(defs)) }
 for _, d := range defs {
  if !json.Valid(d.InputSchema) { t.Errorf("%s has invalid schema", d.Name) }
  var s map[string]any
  if err := json.Unmarshal(d.InputSchema, &s); err != nil { t.Fatal(err) }
  if s["type"] != "object" || s["additionalProperties"] != false { t.Errorf("%s is not strict", d.Name) }
 }
}
func toolDef(name string) ToolDefinition { for _, d := range FuzeCLIToolDefinitions() { if d.Name == name { return d } }; return ToolDefinition{} }
func TestToolSchemaRejectsInvalidArguments(t *testing.T) {
 d := toolDef("workspace.read_file")
 for _, raw := range []string{`{}`, `{"path":"a.go","extra":true}`, `{"path":""}`, `{"path":"a.go","start_line":0}`, `[]`} {
  if err := ValidateToolArguments(d, json.RawMessage(raw)); err == nil { t.Errorf("accepted invalid arguments %s", raw) }
 }
 if err := ValidateToolArguments(d, json.RawMessage(`{"path":"a.go","start_line":2,"end_line":4}`)); err != nil { t.Fatal(err) }
}
func TestToolSchemaEditConditionalRules(t *testing.T) {
 d := toolDef("workspace.apply_edits")
 cases := []struct{raw, want string}{
  {`{"files":[{"path":"a.txt","action":"create"}]}`, "content is required"},
  {`{"files":[{"path":"a.txt","action":"delete","content":""}]}`, "content must be absent"},
  {`{"files":[{"path":"a.txt","action":"modify","content":""}]}`, ""},
 }
 for _, tc := range cases {
  err := ValidateToolArguments(d, json.RawMessage(tc.raw))
  if tc.want == "" && err != nil { t.Errorf("valid edit rejected: %v", err) }
  if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) { t.Errorf("wanted %q, got %v", tc.want, err) }
 }
}
func TestToolRegistryValidatesSchemaBeforeHandler(t *testing.T) {
 r := NewToolRegistry()
 d := toolDef("workspace.read_file")
 called := false
 if err := r.Register(d, func(context.Context,json.RawMessage)(any,error){ called=true; return map[string]bool{"ok":true},nil }); err != nil { t.Fatal(err) }
 got := r.Execute(context.Background(), ToolCall{ID:"bad",Name:d.Name,Arguments:json.RawMessage(`{"path":"a.txt","unexpected":true}`)}, nil)
 if got.Status != ToolResultError || called { t.Fatalf("invalid call reached handler: %#v", got) }
 got = r.Execute(context.Background(), ToolCall{ID:"good",Name:d.Name,Arguments:json.RawMessage(`{"path":"a.txt"}`)}, nil)
 if got.Status != ToolResultSuccess || !called { t.Fatalf("valid call failed: %#v", got) }
}

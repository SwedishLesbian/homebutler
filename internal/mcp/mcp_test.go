package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Higangssh/homebutler/internal/config"
)

func newTestServer() (*Server, *bytes.Buffer) {
	cfg := &config.Config{
		Alerts: config.AlertConfig{CPU: 90, Memory: 85, Disk: 90},
		Wake: []config.WakeTarget{
			{Name: "nas", MAC: "AA:BB:CC:DD:EE:FF", Broadcast: "192.168.1.255"},
		},
	}
	out := &bytes.Buffer{}
	s := NewServer(cfg, "test")
	s.out = out
	return s, out
}

func sendAndReceive(t *testing.T, s *Server, out *bytes.Buffer, request string) jsonRPCResponse {
	t.Helper()
	s.in = strings.NewReader(request + "\n")
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response %q: %v", out.String(), err)
	}
	return resp
}

func TestInitialize(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var init initializeResult
	if err := json.Unmarshal(result, &init); err != nil {
		t.Fatalf("unmarshal initializeResult: %v", err)
	}

	if init.ProtocolVersion != "2024-11-05" {
		t.Errorf("protocolVersion = %q, want %q", init.ProtocolVersion, "2024-11-05")
	}
	if init.ServerInfo.Name != "homebutler" {
		t.Errorf("serverInfo.name = %q, want %q", init.ServerInfo.Name, "homebutler")
	}
	if init.Capabilities.Tools == nil {
		t.Error("capabilities.tools should not be nil")
	}
}

func TestNotificationsInitialized(t *testing.T) {
	s, out := newTestServer()
	// Notification has no id, should produce no response
	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	s.in = strings.NewReader(req + "\n")
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for notification, got: %s", out.String())
	}
}

func TestToolsList(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var list toolsListResult
	if err := json.Unmarshal(result, &list); err != nil {
		t.Fatalf("unmarshal toolsListResult: %v", err)
	}

	if len(list.Tools) != 14 {
		t.Errorf("expected 14 tools, got %d", len(list.Tools))
	}

	expectedTools := map[string]bool{
		"system_status":     false,
		"docker_list":       false,
		"docker_restart":    false,
		"docker_stop":       false,
		"docker_logs":       false,
		"wake":              false,
		"open_ports":        false,
		"network_scan":      false,
		"alerts":            false,
		"install_list":      false,
		"install_app":       false,
		"install_status":    false,
		"install_uninstall": false,
		"install_purge":     false,
	}

	for _, tool := range list.Tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %s inputSchema.type = %q, want %q", tool.Name, tool.InputSchema.Type, "object")
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found", name)
		}
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var callResult toolsCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		t.Fatalf("unmarshal toolsCallResult: %v", err)
	}

	if !callResult.IsError {
		t.Error("expected isError=true for unknown tool")
	}
	if len(callResult.Content) == 0 || !strings.Contains(callResult.Content[0].Text, "unknown tool") {
		t.Error("expected error message about unknown tool")
	}
}

func TestToolsCallMissingRequired(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"docker_restart","arguments":{}}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var callResult toolsCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		t.Fatalf("unmarshal toolsCallResult: %v", err)
	}

	if !callResult.IsError {
		t.Error("expected isError=true for missing required param")
	}
	if len(callResult.Content) == 0 || !strings.Contains(callResult.Content[0].Text, "missing required") {
		t.Error("expected error message about missing required parameter")
	}
}

func TestUnknownMethod(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":5,"method":"unknown/method","params":{}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32601)
	}
}

func TestInvalidJSON(t *testing.T) {
	s, out := newTestServer()
	s.in = strings.NewReader("not json\n")
	out.Reset()
	_ = s.Run()

	var resp jsonRPCResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want %d", resp.Error.Code, -32700)
	}
}

func TestToolsCallRemoteServerNotFound(t *testing.T) {
	s, out := newTestServer()
	req := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"system_status","arguments":{"server":"nonexistent"}}}`
	resp := sendAndReceive(t, s, out, req)

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	result, _ := json.Marshal(resp.Result)
	var callResult toolsCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		t.Fatalf("unmarshal toolsCallResult: %v", err)
	}

	if !callResult.IsError {
		t.Error("expected isError=true for unknown server")
	}
	if len(callResult.Content) == 0 || !strings.Contains(callResult.Content[0].Text, "not found") {
		t.Error("expected error message about server not found")
	}
}

func TestMultipleRequests(t *testing.T) {
	s, out := newTestServer()
	lines := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}, "\n")
	s.in = strings.NewReader(lines + "\n")
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Should have exactly 2 responses (initialize and tools/list — notification produces none)
	outputLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(outputLines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %s", len(outputLines), out.String())
	}

	// First response: initialize
	var resp1 jsonRPCResponse
	if err := json.Unmarshal([]byte(outputLines[0]), &resp1); err != nil {
		t.Fatalf("parse response 1: %v", err)
	}
	if resp1.Error != nil {
		t.Errorf("response 1 unexpected error: %v", resp1.Error)
	}

	// Second response: tools/list
	var resp2 jsonRPCResponse
	if err := json.Unmarshal([]byte(outputLines[1]), &resp2); err != nil {
		t.Fatalf("parse response 2: %v", err)
	}
	if resp2.Error != nil {
		t.Errorf("response 2 unexpected error: %v", resp2.Error)
	}
}

func TestEmptyLines(t *testing.T) {
	s, out := newTestServer()
	input := "\n\n" + `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n\n"
	s.in = strings.NewReader(input)
	out.Reset()
	if err := s.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	outputLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(outputLines) != 1 {
		t.Fatalf("expected 1 response line, got %d", len(outputLines))
	}
}

func TestToolDefinitionsHaveRequiredFields(t *testing.T) {
	tools := toolDefinitions()
	requireMap := map[string][]string{
		"docker_restart":    {"name"},
		"docker_stop":       {"name"},
		"docker_logs":       {"name"},
		"wake":              {"target"},
		"install_app":       {"app"},
		"install_status":    {"app"},
		"install_uninstall": {"app"},
		"install_purge":     {"app"},
	}

	for _, tool := range tools {
		expected, hasRequired := requireMap[tool.Name]
		if hasRequired {
			if len(tool.InputSchema.Required) != len(expected) {
				t.Errorf("tool %s: expected %d required fields, got %d", tool.Name, len(expected), len(tool.InputSchema.Required))
			}
			for _, req := range expected {
				found := false
				for _, r := range tool.InputSchema.Required {
					if r == req {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("tool %s: missing required field %q", tool.Name, req)
				}
			}
		} else {
			if len(tool.InputSchema.Required) != 0 {
				t.Errorf("tool %s: expected no required fields, got %v", tool.Name, tool.InputSchema.Required)
			}
		}
	}
}

func TestStringArg(t *testing.T) {
	args := map[string]any{
		"str":   "hello",
		"num":   float64(42),
		"float": float64(3.14),
	}

	if v := stringArg(args, "str"); v != "hello" {
		t.Errorf("stringArg(str) = %q, want %q", v, "hello")
	}
	if v := stringArg(args, "num"); v != "42" {
		t.Errorf("stringArg(num) = %q, want %q", v, "42")
	}
	if v := stringArg(args, "missing"); v != "" {
		t.Errorf("stringArg(missing) = %q, want empty", v)
	}
	if v := stringArg(nil, "key"); v != "" {
		t.Errorf("stringArg(nil, key) = %q, want empty", v)
	}
}

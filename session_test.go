// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/rakyll/agent-substrate-env/config"
)

func TestSessionManager_Execute(t *testing.T) {
	// Spin up a test server to mock atenet HTTP router
	var receivedReq mockProcessRequest
	var receivedHost string
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		if r.URL.Path == "/process" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.Unmarshal(body, &receivedReq)

			// Return mock process output
			resp := mockProcessResponse{
				Stdout:   "mock stdout output",
				Stderr:   "mock stderr output",
				ExitCode: 0,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer testServer.Close()

	// Parse host:port from testServer.URL (skip http://)
	atenetAddr := testServer.URL[len("http://"):]

	store := NewSessionManager("localhost:8080", atenetAddr, "default")
	sessionID := "test-session-123"

	// Register session manually in the store
	store.sessions[sessionID] = &Session{
		SessionID:    sessionID,
		TemplateName: "bash-env",
		EnvVars: map[string]string{
			"SESSION_VAR": "session_val",
		},
	}

	// 1. Test "bash" tool call
	t.Run("bash tool", func(t *testing.T) {
		inputs := []ToolCall{
			ToolCall{
				ID:   "call-1",
				Type: "function",
				Function: FunctionCall{
					Name:      "bash",
					Arguments: `{"command": "echo hello"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		if resps[0].CallID != "call-1" {
			t.Errorf("Expected call_id 'call-1', got %s", resps[0].CallID)
		}

		// Assert command sent to atenet
		if len(receivedReq.Command) != 3 || receivedReq.Command[0] != "sh" || receivedReq.Command[2] != "echo hello" {
			t.Errorf("Unexpected command: %v", receivedReq.Command)
		}

		// Assert environment variables merged
		if receivedReq.EnvVars["SESSION_VAR"] != "session_val" {
			t.Errorf("Expected SESSION_VAR to be 'session_val', got '%s'", receivedReq.EnvVars["SESSION_VAR"])
		}

		// Assert Host header set correctly
		expectedHost := "test-session-123.actors.resources.substrate.ate.dev"
		if receivedHost != expectedHost {
			t.Errorf("Expected Host header '%s', got '%s'", expectedHost, receivedHost)
		}
	})

	// 2. Test "write_file" tool call
	t.Run("write_file tool", func(t *testing.T) {
		inputs := []ToolCall{
			ToolCall{
				ID:   "call-2",
				Type: "function",
				Function: FunctionCall{
					Name:      "write_file",
					Arguments: `{"path": "/src/main.go", "content": "package main"}`,
				},
			},
		}

		_, err := store.Execute(context.Background(), sessionID, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if receivedReq.EnvVars["FILE_PATH"] != "/src/main.go" {
			t.Errorf("Expected FILE_PATH to be '/src/main.go', got '%s'", receivedReq.EnvVars["FILE_PATH"])
		}
		if receivedReq.EnvVars["FILE_CONTENT"] != "package main" {
			t.Errorf("Expected FILE_CONTENT to be 'package main', got '%s'", receivedReq.EnvVars["FILE_CONTENT"])
		}

		foundWrite := false
		for _, arg := range receivedReq.Command {
			if strings.Contains(arg, "printf '%s' \"$FILE_CONTENT\" > \"$FILE_PATH\"") {
				foundWrite = true
				break
			}
		}
		if !foundWrite {
			t.Errorf("Expected write command template in: %v", receivedReq.Command)
		}
	})

	// 3. Test "read_file" tool call
	t.Run("read_file tool", func(t *testing.T) {
		inputs := []ToolCall{
			ToolCall{
				ID:   "call-3",
				Type: "function",
				Function: FunctionCall{
					Name:      "read_file",
					Arguments: `{"path": "/src/main.go"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		if resps[0].CallID != "call-3" {
			t.Errorf("Expected call_id 'call-3', got %s", resps[0].CallID)
		}

		if receivedReq.EnvVars["FILE_PATH"] != "/src/main.go" {
			t.Errorf("Expected FILE_PATH to be '/src/main.go', got '%s'", receivedReq.EnvVars["FILE_PATH"])
		}

		foundRead := false
		for _, arg := range receivedReq.Command {
			if strings.Contains(arg, "cat \"$FILE_PATH\"") {
				foundRead = true
				break
			}
		}
		if !foundRead {
			t.Errorf("Expected read command template in: %v", receivedReq.Command)
		}
	})

	// 4. Test unsupported tool call returns error response
	t.Run("unsupported tool call", func(t *testing.T) {
		inputs := []ToolCall{
			ToolCall{
				ID:   "call-4",
				Type: "function",
				Function: FunctionCall{
					Name:      "custom_unsupported_tool",
					Arguments: `{"arg": "value"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		if resps[0].CallID != "call-4" {
			t.Errorf("Expected call_id 'call-4', got %s", resps[0].CallID)
		}

		expectedErr := "Error: unsupported tool 'custom_unsupported_tool'"
		if resps[0].Content != expectedErr {
			t.Errorf("Expected response content '%s', got '%s'", expectedErr, resps[0].Content)
		}
	})
}

type mockProcessRequest struct {
	Command []string          `json:"command"`
	EnvVars map[string]string `json:"envvars,omitempty"`
}

type mockProcessResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"`
}

func TestLoadYAMLConfig(t *testing.T) {
	yamlData := `
listen: ":9090"
ate:
  ateapi: "grpc.example.com:443"
  atenet: "http.example.com"
  namespace: "my-custom-ns"
`
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(yamlData)); err != nil {
		t.Fatalf("failed to write config data: %v", err)
	}
	tmpFile.Close()

	cfg, err := config.Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to load YAML config: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen ':9090', got '%s'", cfg.Listen)
	}
	if cfg.Ate.Ateapi != "grpc.example.com:443" {
		t.Errorf("expected ateapi 'grpc.example.com:443', got '%s'", cfg.Ate.Ateapi)
	}
	if cfg.Ate.Atenet != "http.example.com" {
		t.Errorf("expected atenet 'http.example.com', got '%s'", cfg.Ate.Atenet)
	}
	if cfg.Ate.Namespace != "my-custom-ns" {
		t.Errorf("expected namespace 'my-custom-ns', got '%s'", cfg.Ate.Namespace)
	}
}

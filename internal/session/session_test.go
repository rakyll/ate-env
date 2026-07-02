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

package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rakyll/agent-substrate-env/internal/config"
)

func TestSessionManager_Execute(t *testing.T) {
	store := NewSessionManager("localhost:8080", "default", map[string]EnvDetails{
		"bash-env": {
			TemplateName: "bash-env-template",
			Tools:        []string{"bash", "read_file", "write_file"},
		},
	})
	sessionID := "test-session-123"

	envVars := []EnvVariable{
		{Name: "SESSION_VAR", Value: "session_val"},
	}

	// 1. Test "bash" tool call runs locally and returns stdout.
	t.Run("bash tool", func(t *testing.T) {
		inputs := []ToolCall{
			{
				ID:   "call-1",
				Type: "function",
				Function: FunctionCall{
					Name:      "bash",
					Arguments: `{"command": "echo hello $SESSION_VAR"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, "bash-env", envVars, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		if resps[0].CallID != "call-1" {
			t.Errorf("Expected call_id 'call-1', got %s", resps[0].CallID)
		}

		// The command runs locally with the per-call env var merged in.
		if resps[0].Content != "hello session_val\n" {
			t.Errorf("Expected content 'hello session_val\\n', got %q", resps[0].Content)
		}
	})

	// File operations run in-process against the local filesystem, so they
	// operate under a temp directory rather than the mock actor endpoint.
	fileDir := t.TempDir()
	filePath := filepath.Join(fileDir, "src", "main.go")

	// 2. Test "write_file" tool call
	t.Run("write_file tool", func(t *testing.T) {
		inputs := []ToolCall{
			{
				ID:   "call-2",
				Type: "function",
				Function: FunctionCall{
					Name:      "write_file",
					Arguments: `{"path": "` + filePath + `", "content": "package main"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, "bash-env", nil, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		// The file (and its parent directory) should now exist on disk.
		got, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("expected file to be written: %v", err)
		}
		if string(got) != "package main" {
			t.Errorf("Expected file content 'package main', got '%s'", string(got))
		}
	})

	// 3. Test "read_file" tool call
	t.Run("read_file tool", func(t *testing.T) {
		inputs := []ToolCall{
			{
				ID:   "call-3",
				Type: "function",
				Function: FunctionCall{
					Name:      "read_file",
					Arguments: `{"path": "` + filePath + `"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, "bash-env", nil, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		if resps[0].CallID != "call-3" {
			t.Errorf("Expected call_id 'call-3', got %s", resps[0].CallID)
		}

		if resps[0].Content != "package main" {
			t.Errorf("Expected content 'package main', got '%s'", resps[0].Content)
		}
	})

	// 4. Test unsupported tool call returns error response
	t.Run("unsupported tool call", func(t *testing.T) {
		inputs := []ToolCall{
			{
				ID:   "call-4",
				Type: "function",
				Function: FunctionCall{
					Name:      "custom_unsupported_tool",
					Arguments: `{"arg": "value"}`,
				},
			},
		}

		resps, err := store.Execute(context.Background(), sessionID, "bash-env", nil, inputs)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if len(resps) != 1 {
			t.Fatalf("Expected 1 response, got %d", len(resps))
		}

		if resps[0].CallID != "call-4" {
			t.Errorf("Expected call_id 'call-4', got %s", resps[0].CallID)
		}

		expectedErr := "Error: tool 'custom_unsupported_tool' is not enabled in environment 'bash-env'"
		if resps[0].Content != expectedErr {
			t.Errorf("Expected response content '%s', got '%s'", expectedErr, resps[0].Content)
		}
	})
}

func TestLoadYAMLConfig(t *testing.T) {
	yamlData := `
listen: ":9090"
ate:
  ateapi: "grpc.example.com:443"
  atespace: "my-custom-ns"
environments:
  - name: "bash-env"
    template: "bash-env-template"
    enabled_tools:
      - "bash"
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
	if cfg.Ate.Atespace != "my-custom-ns" {
		t.Errorf("expected atespace 'my-custom-ns', got '%s'", cfg.Ate.Atespace)
	}
	if len(cfg.Environments) != 1 {
		t.Fatalf("expected 1 environment, got %d", len(cfg.Environments))
	}
	env := cfg.Environments[0]
	if env.Name != "bash-env" || env.Template != "bash-env-template" {
		t.Errorf("unexpected environment mapping: %+v", env)
	}
	if len(env.EnabledTools) != 1 || env.EnabledTools[0] != "bash" {
		t.Errorf("unexpected environment tools: %v", env.EnabledTools)
	}
}

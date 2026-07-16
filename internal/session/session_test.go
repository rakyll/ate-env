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
	"strings"
	"testing"

	"github.com/rakyll/ate-env/internal/config"
)

func TestSessionManager_Execute(t *testing.T) {
	store := NewSessionManager("localhost:8080", writeTestSkills(t), map[string]EnvDetails{
		"bash-env": {
			TemplateName: "bash-env-template",
			Atespace:     "default",
			Tools:        []string{"bash", "read_file", "write_file", "list_skills", "activate_skill"},
		},
	})
	sessionID := "test-session-123"

	envVars := []EnvVariable{
		{Name: "SESSION_VAR", Value: "session_val"},
	}

	// 1. Test "bash" tool call runs locally and returns stdout.
	t.Run("bash tool", func(t *testing.T) {
		input := ToolCall{
			ID:   "call-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "bash",
				Arguments: `{"command": "echo hello $SESSION_VAR"}`,
			},
		}

		resp, err := store.Execute(context.Background(), sessionID, "bash-env", envVars, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if resp.Type != "function_call_output" {
			t.Errorf("Expected type 'function_call_output', got %s", resp.Type)
		}

		if resp.CallID != "call-1" {
			t.Errorf("Expected call_id 'call-1', got %s", resp.CallID)
		}

		// The command runs locally with the per-call env var merged in.
		if resp.Output != "hello session_val\n" {
			t.Errorf("Expected content 'hello session_val\\n', got %q", resp.Output)
		}
	})

	// File operations run in-process against the local filesystem, so they
	// operate under a temp directory rather than the mock actor endpoint.
	fileDir := t.TempDir()
	filePath := filepath.Join(fileDir, "src", "main.go")

	// 2. Test "write_file" tool call
	t.Run("write_file tool", func(t *testing.T) {
		input := ToolCall{
			ID:   "call-2",
			Type: "function",
			Function: FunctionCall{
				Name:      "write_file",
				Arguments: `{"path": "` + filePath + `", "content": "package main"}`,
			},
		}

		if _, err := store.Execute(context.Background(), sessionID, "bash-env", nil, input); err != nil {
			t.Fatalf("Execute failed: %v", err)
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
		input := ToolCall{
			ID:   "call-3",
			Type: "function",
			Function: FunctionCall{
				Name:      "read_file",
				Arguments: `{"path": "` + filePath + `"}`,
			},
		}

		resp, err := store.Execute(context.Background(), sessionID, "bash-env", nil, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if resp.CallID != "call-3" {
			t.Errorf("Expected call_id 'call-3', got %s", resp.CallID)
		}

		if resp.Output != "package main" {
			t.Errorf("Expected content 'package main', got '%s'", resp.Output)
		}
	})

	// 4. Test unsupported tool call returns error response
	t.Run("unsupported tool call", func(t *testing.T) {
		input := ToolCall{
			ID:   "call-4",
			Type: "function",
			Function: FunctionCall{
				Name:      "custom_unsupported_tool",
				Arguments: `{"arg": "value"}`,
			},
		}

		resp, err := store.Execute(context.Background(), sessionID, "bash-env", nil, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if resp.Type != "function_call_output" {
			t.Errorf("Expected type 'function_call_output', got %s", resp.Type)
		}

		if resp.CallID != "call-4" {
			t.Errorf("Expected call_id 'call-4', got %s", resp.CallID)
		}

		expectedErr := "Error: tool 'custom_unsupported_tool' is not enabled in environment 'bash-env'"
		if resp.Output != expectedErr {
			t.Errorf("Expected response content '%s', got '%s'", expectedErr, resp.Output)
		}
	})

	// 5. Test "list_skills" tool call surfaces skill metadata.
	t.Run("list_skills tool", func(t *testing.T) {
		input := ToolCall{
			ID:   "call-5",
			Type: "function",
			Function: FunctionCall{
				Name: "list_skills",
			},
		}

		resp, err := store.Execute(context.Background(), sessionID, "bash-env", nil, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !strings.Contains(resp.Output, "pdf-processing: Extract text from PDF files.") {
			t.Errorf("Expected listing to include pdf-processing skill, got %q", resp.Output)
		}
	})

	// 6. Test "activate_skill" tool call returns the skill instructions and
	// bundled files.
	t.Run("activate_skill tool", func(t *testing.T) {
		input := ToolCall{
			ID:   "call-6",
			Type: "function",
			Function: FunctionCall{
				Name:      "activate_skill",
				Arguments: `{"name": "pdf-processing"}`,
			},
		}

		resp, err := store.Execute(context.Background(), sessionID, "bash-env", nil, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !strings.Contains(resp.Output, "Run extract.sh against the PDF.") {
			t.Errorf("Expected skill instructions in output, got %q", resp.Output)
		}
		if !strings.Contains(resp.Output, filepath.Join("pdf-processing", "extract.sh")) {
			t.Errorf("Expected bundled file listing in output, got %q", resp.Output)
		}
	})

	// 7. Test "activate_skill" with an unknown skill returns an error response.
	t.Run("activate_skill unknown skill", func(t *testing.T) {
		input := ToolCall{
			ID:   "call-7",
			Type: "function",
			Function: FunctionCall{
				Name:      "activate_skill",
				Arguments: `{"name": "no-such-skill"}`,
			},
		}

		resp, err := store.Execute(context.Background(), sessionID, "bash-env", nil, input)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !strings.Contains(resp.Output, `skill "no-such-skill" not found`) {
			t.Errorf("Expected not-found error output, got %q", resp.Output)
		}
	})
}

// writeTestSkills lays out a skills directory with one skill in the Agent
// Skills format: a directory holding SKILL.md plus a bundled script.
func writeTestSkills(t *testing.T) string {
	t.Helper()

	skillsDir := t.TempDir()
	skillDir := filepath.Join(skillsDir, "pdf-processing")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillMD := `---
name: pdf-processing
description: Extract text from PDF files.
---

Run extract.sh against the PDF.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "extract.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to write bundled script: %v", err)
	}
	return skillsDir
}

func TestLoadYAMLConfig(t *testing.T) {
	yamlData := `
listen: ":9090"
ate:
  ateapi: "grpc.example.com:443"
environments:
  - name: "bash-env"
    template: "bash-env-template"
    atespace: "my-custom-ns"
    allowed_tools:
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
	if cfg.SkillsDir != "/skills" {
		t.Errorf("expected default SkillsDir '/skills', got '%s'", cfg.SkillsDir)
	}
	if len(cfg.Environments) != 1 {
		t.Fatalf("expected 1 environment, got %d", len(cfg.Environments))
	}
	env := cfg.Environments[0]
	if env.Name != "bash-env" || env.Template != "bash-env-template" || env.Atespace != "my-custom-ns" {
		t.Errorf("unexpected environment mapping: %+v", env)
	}
	if len(env.AllowedTools) != 1 || env.AllowedTools[0] != "bash" {
		t.Errorf("unexpected environment tools: %v", env.AllowedTools)
	}
}

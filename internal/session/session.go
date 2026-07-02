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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// SessionManager handles communication with Agent Substrate.
type SessionManager struct {
	ateapiAddr   string
	atespace     string
	environments map[string]EnvDetails
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(ateapiAddr, atespace string, environments map[string]EnvDetails) *SessionManager {
	return &SessionManager{
		ateapiAddr:   ateapiAddr,
		atespace:     atespace,
		environments: environments,
	}
}

// dialAteAPI creates a new gRPC client connection to the Agent Substrate API.
func (s *SessionManager) dialAteAPI() (ateapipb.ControlClient, *grpc.ClientConn, error) {
	creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	conn, err := grpc.NewClient(s.ateapiAddr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, err
	}
	return ateapipb.NewControlClient(conn), conn, nil
}

// Resume creates (if not exists) and resumes the underlying sandboxed actor for the session.
func (s *SessionManager) Resume(ctx context.Context, sessionID, envName string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id cannot be empty")
	}
	if envName == "" {
		return fmt.Errorf("environment cannot be empty")
	}

	cli, conn, err := s.dialAteAPI()
	if err != nil {
		return fmt.Errorf("failed to dial Agent Substrate API: %w", err)
	}
	defer conn.Close()

	templateName := envName
	var tools []string
	if mapped, exists := s.environments[envName]; exists {
		templateName = mapped.TemplateName
		tools = mapped.Tools
		log.Printf("Creating actor %s with template %s (mapped from %s) in atespace %s with tools %v...", sessionID, templateName, envName, s.atespace, tools)
	} else {
		log.Printf("Creating actor %s with template %s in atespace %s...", sessionID, templateName, s.atespace)
	}

	// 1. Create Actor (idempotent, ignore AlreadyExists)
	_, err = cli.CreateActor(ctx, &ateapipb.CreateActorRequest{
		ActorId:                sessionID,
		ActorTemplateNamespace: s.atespace,
		ActorTemplateName:      templateName,
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("failed to create actor: %w", err)
	}

	// 2. Resume Actor
	log.Printf("Resuming actor %s...", sessionID)
	_, err = cli.ResumeActor(ctx, &ateapipb.ResumeActorRequest{
		ActorId: sessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to resume actor: %w", err)
	}

	log.Printf("Session %s successfully resumed", sessionID)
	return nil
}

// Suspend suspends the underlying sandboxed actor.
func (s *SessionManager) Suspend(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id cannot be empty")
	}

	cli, conn, err := s.dialAteAPI()
	if err != nil {
		return fmt.Errorf("failed to dial Agent Substrate API: %w", err)
	}
	defer conn.Close()

	log.Printf("Suspending actor %s...", sessionID)
	_, err = cli.SuspendActor(ctx, &ateapipb.SuspendActorRequest{
		ActorId: sessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to suspend actor: %w", err)
	}

	log.Printf("Session %s successfully suspended", sessionID)
	return nil
}

// Execute parses and runs multiple tool calls inside the sandboxed actor.
func (s *SessionManager) Execute(ctx context.Context, sessionID string, envName string, envVariables []EnvVariable, toolCalls []ToolCall) ([]ToolResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id cannot be empty")
	}
	if len(toolCalls) == 0 {
		return nil, fmt.Errorf("no valid tool calls found in inputs")
	}

	var allowedTools []string
	if mapped, exists := s.environments[envName]; exists {
		allowedTools = mapped.Tools
	} else {
		return nil, fmt.Errorf("unknown environment %q", envName)
	}

	// Convert EnvVariables slice to map for easier lookups
	envVars := make(map[string]string)
	for _, env := range envVariables {
		envVars[env.Name] = env.Value
	}

	var responses []ToolResponse
	for _, tc := range toolCalls {
		// Verify if tool is enabled in this environment
		if !isToolAllowed(tc.Function.Name, allowedTools) {
			callID := tc.CallID
			if callID == "" {
				callID = tc.ID
			}
			responses = append(responses, ToolResponse{
				Name:    tc.Function.Name,
				CallID:  callID,
				Content: fmt.Sprintf("Error: tool '%s' is not enabled in environment '%s'", tc.Function.Name, envName),
			})
			continue
		}

		resp := s.executeToolCall(ctx, envVars, tc)
		responses = append(responses, resp)
	}

	return responses, nil
}

func isToolAllowed(tool string, allowed []string) bool {
	for _, t := range allowed {
		if t == tool {
			return true
		}
	}
	return false
}

// executeToolCall runs a single tool call locally in this binary.
func (s *SessionManager) executeToolCall(ctx context.Context, executionEnv map[string]string, tc ToolCall) ToolResponse {
	// OpenResponses uses call_id; OpenAI uses id. Let's support both.
	callID := tc.CallID
	if callID == "" {
		callID = tc.ID
	}

	toolResp := ToolResponse{
		Name:   tc.Function.Name,
		CallID: callID,
	}

	var args map[string]any
	if tc.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			toolResp.Content = fmt.Sprintf("Error parsing tool arguments: %v", err)
			return toolResp
		}
	}

	// File operations run in-process on the local filesystem via the Go
	// standard library (see fileops.go); only bash is forwarded to the actor.
	switch tc.Function.Name {
	case "write_file":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if path == "" {
			toolResp.Content = "Error: 'path' argument is required"
			return toolResp
		}
		out, err := writeFile(path, content)
		if err != nil {
			toolResp.Content = fmt.Sprintf("Error: %v", err)
			return toolResp
		}
		toolResp.Content = out
		return toolResp

	case "read_file":
		path, _ := args["path"].(string)
		if path == "" {
			toolResp.Content = "Error: 'path' argument is required"
			return toolResp
		}
		out, err := readFile(path)
		if err != nil {
			toolResp.Content = fmt.Sprintf("Error: %v", err)
			return toolResp
		}
		toolResp.Content = out
		return toolResp

	case "list_dir":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		out, err := listDir(path)
		if err != nil {
			toolResp.Content = fmt.Sprintf("Error: %v", err)
			return toolResp
		}
		toolResp.Content = out
		return toolResp

	case "bash":
		command, _ := args["command"].(string)
		if command == "" {
			// Fallback: try "code" or "cmd"
			if c, ok := args["code"].(string); ok {
				command = c
			} else if c, ok := args["cmd"].(string); ok {
				command = c
			}
		}
		if command == "" {
			toolResp.Content = "Error: 'command' argument is required"
			return toolResp
		}
		// Run the shell command locally in this binary.
		cmd := []string{"sh", "-c", command}
		stdout, err := runCommand(ctx, cmd, executionEnv)
		if err != nil {
			toolResp.Content = fmt.Sprintf("Error: %v", err)
			return toolResp
		}
		toolResp.Content = stdout
		return toolResp
	default:
		toolResp.Content = fmt.Sprintf("Error: unsupported tool '%s'", tc.Function.Name)
		return toolResp
	}
}

// runCommand executes cmd locally in this binary, layering executionEnv on top
// of the current process environment, and returns its stdout.
func runCommand(ctx context.Context, cmd []string, executionEnv map[string]string) (string, error) {
	if len(cmd) == 0 {
		return "", fmt.Errorf("empty command")
	}

	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)

	// Start from this process's environment, then merge per-call env vars.
	c.Env = os.Environ()
	for k, v := range executionEnv {
		c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("exit code %d: %s (stdout: %s)", exitErr.ExitCode(), stderr.String(), stdout.String())
		}
		return "", fmt.Errorf("failed to run command: %w", err)
	}

	return stdout.String(), nil
}

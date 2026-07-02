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
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// Session tracks the metadata and environment configuration for a single session.
type Session struct {
	SessionID    string
	TemplateName string
	EnvVars      map[string]string
	Tools        []string
}

// SessionManager manages active sandboxed sessions and handles communication with Agent Substrate.
type SessionManager struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	ateapiAddr   string
	atenetAddr   string
	ateNamespace string
	environments map[string]string
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(ateapiAddr, ateNamespace string, environments map[string]string) *SessionManager {
	return &SessionManager{
		sessions:     make(map[string]*Session),
		ateapiAddr:   ateapiAddr,
		ateNamespace: ateNamespace,
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
func (s *SessionManager) Resume(ctx context.Context, req ResumeRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("session_id cannot be empty")
	}
	if req.Name == "" {
		return fmt.Errorf("name (template name) cannot be empty")
	}

	cli, conn, err := s.dialAteAPI()
	if err != nil {
		return fmt.Errorf("failed to dial Agent Substrate API: %w", err)
	}
	defer conn.Close()

	templateName := req.Name
	if mapped, exists := s.environments[req.Name]; exists {
		templateName = mapped
		log.Printf("Creating actor %s with template %s (mapped from %s) in namespace %s...", req.SessionID, templateName, req.Name, s.ateNamespace)
	} else {
		log.Printf("Creating actor %s with template %s in namespace %s...", req.SessionID, templateName, s.ateNamespace)
	}

	// 1. Create Actor (idempotent, ignore AlreadyExists)
	_, err = cli.CreateActor(ctx, &ateapipb.CreateActorRequest{
		ActorId:                req.SessionID,
		ActorTemplateNamespace: s.ateNamespace,
		ActorTemplateName:      templateName,
	})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("failed to create actor: %w", err)
	}

	// 2. Resume Actor
	log.Printf("Resuming actor %s...", req.SessionID)
	_, err = cli.ResumeActor(ctx, &ateapipb.ResumeActorRequest{
		ActorId: req.SessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to resume actor: %w", err)
	}

	// Convert EnvVariables slice to map for easier lookups
	envVars := make(map[string]string)
	for _, env := range req.EnvVariables {
		envVars[env.Name] = env.Value
	}

	// 3. Cache session configuration
	s.mu.Lock()
	s.sessions[req.SessionID] = &Session{
		SessionID:    req.SessionID,
		TemplateName: req.Name,
		EnvVars:      envVars,
		Tools:        req.Tools,
	}
	s.mu.Unlock()

	log.Printf("Session %s successfully resumed and cached", req.SessionID)
	return nil
}

// Suspend suspends the underlying sandboxed actor and removes the session from the cache.
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

	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()

	log.Printf("Session %s successfully suspended and removed from cache", sessionID)
	return nil
}

// Execute parses and runs multiple tool calls inside the sandboxed actor.
func (s *SessionManager) Execute(ctx context.Context, sessionID string, toolCalls []ToolCall) ([]ToolResponse, error) {
	s.mu.RLock()
	_, exists := s.sessions[sessionID]
	s.mu.RUnlock()

	if !exists {
		// If it's not in cache, try to resume with a default template name "bash-env" just in case,
		// or return an error. Let's return an error as per standard lifecycle.
		return nil, fmt.Errorf("session %s not initialized or resumed; call /environment/resume first", sessionID)
	}

	if len(toolCalls) == 0 {
		return nil, fmt.Errorf("no valid tool calls found in inputs")
	}

	var responses []ToolResponse
	for _, tc := range toolCalls {
		resp := s.executeToolCall(ctx, sessionID, tc)
		responses = append(responses, resp)
	}

	return responses, nil
}

// executeToolCall routes a single tool call to the actor's /process endpoint.
func (s *SessionManager) executeToolCall(ctx context.Context, sessionID string, tc ToolCall) ToolResponse {
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

	var cmd []string
	var env map[string]string

	switch tc.Function.Name {
	case "write_file":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		if path == "" {
			toolResp.Content = "Error: 'path' argument is required"
			return toolResp
		}
		// Safe write command using environment variables to avoid shell injection
		cmd = []string{"sh", "-c", "mkdir -p $(dirname \"$FILE_PATH\") && printf '%s' \"$FILE_CONTENT\" > \"$FILE_PATH\""}
		env = map[string]string{
			"FILE_PATH":    path,
			"FILE_CONTENT": content,
		}

	case "read_file":
		path, _ := args["path"].(string)
		if path == "" {
			toolResp.Content = "Error: 'path' argument is required"
			return toolResp
		}
		cmd = []string{"sh", "-c", "cat \"$FILE_PATH\""}
		env = map[string]string{
			"FILE_PATH": path,
		}

	case "list_dir":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		cmd = []string{"sh", "-c", "ls -la \"$DIR_PATH\""}
		env = map[string]string{
			"DIR_PATH": path,
		}

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
		cmd = []string{"sh", "-c", command}
	default:
		toolResp.Content = fmt.Sprintf("Error: unsupported tool '%s'", tc.Function.Name)
		return toolResp
	}

	// Execute command via the atenet HTTP router pointing to the actor
	stdout, err := s.executeInActor(ctx, sessionID, cmd, env)
	if err != nil {
		toolResp.Content = fmt.Sprintf("Error: %v", err)
		return toolResp
	}

	toolResp.Content = stdout
	return toolResp
}

// executeInActor sends a process execution request to the actor's /process HTTP endpoint.
func (s *SessionManager) executeInActor(ctx context.Context, sessionID string, cmd []string, customEnv map[string]string) (string, error) {
	host := s.atenetAddr
	if host == "" {
		host = fmt.Sprintf("%s.actors.resources.substrate.ate.dev", sessionID)
	}
	url := fmt.Sprintf("http://%s/process", host)

	// Merge session environment variables and custom tool environment variables
	envVars := make(map[string]string)
	s.mu.RLock()
	sess, exists := s.sessions[sessionID]
	if exists {
		for k, v := range sess.EnvVars {
			envVars[k] = v
		}
	}
	s.mu.RUnlock()

	for k, v := range customEnv {
		envVars[k] = v
	}

	reqBody := struct {
		Command []string          `json:"command"`
		EnvVars map[string]string `json:"envvars,omitempty"`
	}{
		Command: cmd,
		EnvVars: envVars,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Set the Host header so the atenet HTTP router can correctly route to this actor
	req.Host = fmt.Sprintf("%s.actors.resources.substrate.ate.dev", sessionID)

	// Set a reasonable timeout for execution requests if context does not have one
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request to actor: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("actor returned status %d: %s", resp.StatusCode, string(body))
	}

	var processResp struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
		Error    string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&processResp); err != nil {
		return "", fmt.Errorf("failed to decode process response: %w", err)
	}

	if processResp.Error != "" {
		return "", fmt.Errorf("process error: %s (stderr: %s)", processResp.Error, processResp.Stderr)
	}
	if processResp.ExitCode != 0 {
		return "", fmt.Errorf("exit code %d: %s (stdout: %s)", processResp.ExitCode, processResp.Stderr, processResp.Stdout)
	}

	return processResp.Stdout, nil
}

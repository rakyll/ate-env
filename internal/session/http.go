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

// EnvVariable represents a key-value environment variable pair.
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ResumeRequest is the payload for POST /environment/resume.
type ResumeRequest struct {
	Name         string        `json:"name"` // Template name, e.g. "bash-env"
	SessionID    string        `json:"session_id"`
	EnvVariables []EnvVariable `json:"env_variables"`
	Tools        []string      `json:"tools"` // enabled tools
}

// SuspendRequest is the payload for POST /environment/suspend.
type SuspendRequest struct {
	SessionID string `json:"session_id"`
}

// ExecuteRequest is the payload for POST /environment.
type ExecuteRequest struct {
	SessionID string     `json:"session_id"`
	Inputs    []ToolCall `json:"inputs"`
}

// ExecuteResponse is the response payload for POST /environment.
type ExecuteResponse struct {
	Outputs []ToolResponse `json:"outputs"`
}

// ToolCall represents a requested tool call.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	CallID   string       `json:"call_id,omitempty"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the name and arguments of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

// ToolResponse represents a tool execution response in OpenResponses/OpenAI format.
type ToolResponse struct {
	Name    string `json:"name,omitempty"`
	CallID  string `json:"call_id,omitempty"` // OpenResponses format
	Content string `json:"content"`           // The result/output of the tool call
}

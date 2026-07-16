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

// EnvDetails holds predefined environment details.
type EnvDetails struct {
	TemplateName string
	Atespace     string
	Tools        []string
}

// ToolRequest is the payload for
// POST /v1/environments/{env}/sessions/{session_id}.
//
// The environment and session are taken from the URL path, so the body only
// carries the per-call execution data. Each request executes exactly one tool
// call, whose fields are inlined into the body alongside env_variables, and
// the response is the single ToolResponse for it.
type ToolRequest struct {
	EnvVariables []EnvVariable `json:"env_variables"`
	ToolCall
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
	Type   string `json:"type"`
	Name   string `json:"name,omitempty"`
	CallID string `json:"call_id,omitempty"`
	Output string `json:"output"`
}

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

//go:build integration

// Integration tests that exercise the service end-to-end against a real
// Agent Substrate deployment. They are compiled only with the "integration"
// build tag, and the session lifecycle test additionally skips unless
// ATE_TEST_ATEAPI points at a reachable control API, e.g. after:
//
//	kubectl port-forward -n ate-system svc/api 8443:443
//
//	make test-integration ATE_TEST_ATESPACE=ax ATE_TEST_TEMPLATE=ax-harness-template
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/rakyll/ate-env/internal/session"
)

// testEnv holds the coordinates of the real Agent Substrate deployment the
// integration tests run against.
type testEnv struct {
	ateapiAddr string
	atespace   string
	template   string
}

// realSubstrate reads the target deployment from the environment, skipping
// the test when no control API endpoint is configured.
func realSubstrate(t *testing.T) testEnv {
	t.Helper()

	addr := os.Getenv("ATE_TEST_ATEAPI")
	if addr == "" {
		t.Skip("ATE_TEST_ATEAPI not set; skipping integration test against a real Agent Substrate deployment")
	}
	env := testEnv{
		ateapiAddr: addr,
		atespace:   os.Getenv("ATE_TEST_ATESPACE"),
		template:   os.Getenv("ATE_TEST_TEMPLATE"),
	}
	if env.atespace == "" {
		env.atespace = "default"
	}
	if env.template == "" {
		env.template = "bash-env-template"
	}
	return env
}

// controlClient dials the real control API directly, the same way the service
// does, so the test can independently verify and clean up actor state.
func controlClient(t *testing.T, addr string) ateapipb.ControlClient {
	t.Helper()

	creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("failed to dial control API at %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	return ateapipb.NewControlClient(conn)
}

// startService wires the full stack the way runServe does: config file →
// newSessionManager → newMux → HTTP server, pointed at the real control API.
func startService(t *testing.T, env testEnv) *httptest.Server {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := fmt.Sprintf(`
listen: ":0"
ate:
  ateapi: %q
environments:
  - name: "bash-env"
    template: %q
    atespace: %q
    allowed_tools:
      - "bash"
      - "read_file"
      - "write_file"
      - "list_dir"
`, env.ateapiAddr, env.template, env.atespace)
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, sm, err := newSessionManager(configPath)
	if err != nil {
		t.Fatalf("failed to build session manager: %v", err)
	}

	srv := httptest.NewServer(newMux(sm))
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url string, body string) (int, []byte) {
	t.Helper()

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return resp.StatusCode, data
}

// waitForActorStatus polls GetActor until the actor reaches one of the wanted
// statuses, since lifecycle transitions settle asynchronously.
func waitForActorStatus(t *testing.T, cli ateapipb.ControlClient, atespace, actorID string, want ...ateapipb.Actor_Status) *ateapipb.Actor {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	var last *ateapipb.Actor
	for time.Now().Before(deadline) {
		resp, err := cli.GetActor(context.Background(), &ateapipb.GetActorRequest{
			ActorRef: &ateapipb.ActorRef{
				Atespace: atespace,
				Name:     actorID,
			},
		})
		if err == nil {
			last = resp.GetActor()
			if slices.Contains(want, last.GetStatus()) {
				return last
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("actor %s did not reach status %v in time; last seen: %v", actorID, want, last.GetStatus())
	return nil
}

func TestIntegration_SessionLifecycle(t *testing.T) {
	env := realSubstrate(t)
	srv := startService(t, env)
	cli := controlClient(t, env.ateapiAddr)

	// Actor ids must be unique per run (the actor outlives the test binary on
	// failure) and valid DNS-1123 labels.
	sessionID := fmt.Sprintf("ate-env-it-%d", time.Now().UnixNano())
	sessionURL := srv.URL + "/v1/environments/bash-env/sessions/" + sessionID
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := cli.DeleteActor(ctx, &ateapipb.DeleteActorRequest{
			ActorRef: &ateapipb.ActorRef{
				Atespace: env.atespace,
				Name:     sessionID,
			},
		}); err != nil {
			t.Logf("cleanup: failed to delete actor %s: %v", sessionID, err)
		}
	})

	// Resume: the service should create the actor from the mapped template and
	// resume it via the control API.
	code, body := postJSON(t, sessionURL+"/resume", "")
	if code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d: %s", code, body)
	}
	if !bytes.Contains(body, []byte(`"status":"ok"`)) {
		t.Errorf("resume: expected status ok, got %s", body)
	}

	// Verify against the real deployment that the actor exists, was derived
	// from the configured template, and comes up running.
	actor := waitForActorStatus(t, cli, env.atespace, sessionID,
		ateapipb.Actor_STATUS_RUNNING)
	if got := actor.GetActorTemplateName(); got != env.template {
		t.Errorf("resume: expected template %q, got %q", env.template, got)
	}
	if got := actor.GetActorTemplateNamespace(); got != env.atespace {
		t.Errorf("resume: expected atespace %q, got %q", env.atespace, got)
	}

	// Resuming again must succeed: CreateActor returns AlreadyExists, which the
	// service treats as idempotent success.
	code, body = postJSON(t, sessionURL+"/resume", "")
	if code != http.StatusOK {
		t.Fatalf("second resume: expected 200, got %d: %s", code, body)
	}

	// Execute one tool call per request: a bash call with a per-call env var,
	// then a write/read file cycle.
	filePath := filepath.Join(t.TempDir(), "hello.txt")
	fileArgs, _ := json.Marshal(map[string]string{"path": filePath, "content": "written by test"})
	readArgs, _ := json.Marshal(map[string]string{"path": filePath})
	for _, call := range []struct {
		req  session.ToolRequest
		want string
	}{
		{
			req: session.ToolRequest{
				EnvVariables: []session.EnvVariable{{Name: "IT_GREETING", Value: "hola"}},
				ToolCall:     session.ToolCall{CallID: "c1", Type: "function_call", Function: session.FunctionCall{Name: "bash", Arguments: `{"command": "echo $IT_GREETING"}`}},
			},
			want: "hola\n",
		},
		{
			req: session.ToolRequest{
				ToolCall: session.ToolCall{CallID: "c2", Type: "function_call", Function: session.FunctionCall{Name: "write_file", Arguments: string(fileArgs)}},
			},
			want: "", // write_file output is informational; content is checked below
		},
		{
			req: session.ToolRequest{
				ToolCall: session.ToolCall{CallID: "c3", Type: "function_call", Function: session.FunctionCall{Name: "read_file", Arguments: string(readArgs)}},
			},
			want: "written by test",
		},
	} {
		reqBody, _ := json.Marshal(call.req)
		code, body = postJSON(t, sessionURL, string(reqBody))
		if code != http.StatusOK {
			t.Fatalf("execute %s: expected 200, got %d: %s", call.req.CallID, code, body)
		}
		var out session.ToolResponse
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("execute %s: failed to decode response: %v", call.req.CallID, err)
		}
		if out.Type != "function_call_output" {
			t.Errorf("execute %s: expected type 'function_call_output', got %q", call.req.CallID, out.Type)
		}
		if out.CallID != call.req.CallID {
			t.Errorf("execute %s: expected call_id %q, got %q", call.req.CallID, call.req.CallID, out.CallID)
		}
		if call.want != "" && out.Output != call.want {
			t.Errorf("execute %s: expected output %q, got %q", call.req.CallID, call.want, out.Output)
		}
	}
	if got, err := os.ReadFile(filePath); err != nil || string(got) != "written by test" {
		t.Errorf("execute: expected file written with 'written by test', got %q (err: %v)", got, err)
	}

	// A tool outside the environment's allowlist yields an error output, not an
	// HTTP error.
	code, body = postJSON(t, sessionURL, `{"call_id":"c4","type":"function_call","function":{"name":"web_fetcher","arguments":"{}"}}`)
	if code != http.StatusOK {
		t.Fatalf("execute disallowed tool: expected 200, got %d: %s", code, body)
	}
	var disallowed session.ToolResponse
	if err := json.Unmarshal(body, &disallowed); err != nil {
		t.Fatalf("execute disallowed tool: failed to decode response: %v", err)
	}
	if !strings.Contains(disallowed.Output, "not enabled in environment 'bash-env'") {
		t.Errorf("execute disallowed tool: expected not-enabled error output, got %s", body)
	}

	// An unknown environment is rejected outright.
	code, _ = postJSON(t, srv.URL+"/v1/environments/no-such-env/sessions/"+sessionID,
		`{"call_id":"c5","type":"function_call","function":{"name":"bash","arguments":"{\"command\":\"true\"}"}}`)
	if code != http.StatusInternalServerError {
		t.Errorf("execute unknown env: expected 500, got %d", code)
	}

	// Suspend: the service should suspend the actor via the control API.
	code, body = postJSON(t, sessionURL+"/suspend", "")
	if code != http.StatusOK {
		t.Fatalf("suspend: expected 200, got %d: %s", code, body)
	}
	waitForActorStatus(t, cli, env.atespace, sessionID,
		ateapipb.Actor_STATUS_SUSPENDING, ateapipb.Actor_STATUS_SUSPENDED)
}

func TestIntegration_ResumeFailsWhenControlAPIUnavailable(t *testing.T) {
	// Point the service at a port with nothing listening: the lifecycle
	// endpoints, which depend on the control API, must surface the failure.
	// This needs no deployment, so it always runs.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := lis.Addr().String()
	lis.Close()

	srv := startService(t, testEnv{ateapiAddr: addr, atespace: "default", template: "bash-env-template"})
	sessionURL := srv.URL + "/v1/environments/bash-env/sessions/it-session-2"

	code, body := postJSON(t, sessionURL+"/resume", "")
	if code != http.StatusInternalServerError {
		t.Fatalf("resume: expected 500 when control API is down, got %d: %s", code, body)
	}
	if !strings.Contains(string(body), "failed to resume session") {
		t.Errorf("resume: expected failure message, got %s", body)
	}

	code, body = postJSON(t, sessionURL+"/suspend", "")
	if code != http.StatusInternalServerError {
		t.Fatalf("suspend: expected 500 when control API is down, got %d: %s", code, body)
	}
}

func TestIntegration_Healthz(t *testing.T) {
	// Healthz has no control-API dependency, so any address works and the test
	// always runs.
	srv := startService(t, testEnv{ateapiAddr: "localhost:1", atespace: "default", template: "bash-env-template"})

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz: expected 200, got %d", resp.StatusCode)
	}
	var health map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("healthz: failed to decode response: %v", err)
	}
	if health["status"] != "healthy" {
		t.Errorf("healthz: expected status 'healthy', got %q", health["status"])
	}
}

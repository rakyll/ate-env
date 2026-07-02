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
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/rakyll/agent-substrate-env/internal/session"
)

// newServeCmd builds the "serve" subcommand: the environment service. It drives
// actor lifecycle (create, resume, suspend) via the Agent Substrate control API
// and executes tool calls in-process against the local environment.
func newServeCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the environment service (resume, suspend, execute)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(configPath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "config.yaml", "path to the YAML configuration file")
	return cmd
}

func runServe(configPath string) error {
	cfg, store, err := newSessionManager(configPath)
	if err != nil {
		return err
	}

	log.Printf("Starting Agent Substrate environment service...")

	mux := http.NewServeMux()
	// Sessions are sub-resources of an environment. Both the environment and
	// the session id live in the path on every call, which the stateless
	// service needs anyway to pick the template + tool allowlist.
	//
	// Executing tool calls is the primary operation on a session, so it is a
	// POST to the session resource itself. Lifecycle transitions hang off it as
	// trailing action segments (rather than the AIP-style {id}:resume custom
	// method, since net/http requires a path wildcard to span a full segment).
	mux.HandleFunc("POST /v1/environments/{env}/sessions/{session_id}", handleExecute(store))
	mux.HandleFunc("POST /v1/environments/{env}/sessions/{session_id}/resume", handleResume(store))
	mux.HandleFunc("POST /v1/environments/{env}/sessions/{session_id}/suspend", handleSuspend(store))
	mux.HandleFunc("GET /healthz", handleHealthz)

	addr := listenAddr(cfg.Listen)
	log.Printf("Serving HTTP requests on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		return fmt.Errorf("HTTP server failed: %w", err)
	}
	return nil
}

// handleResume handles session resume requests.
func handleResume(store *session.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		envName := r.PathValue("env")
		sessionID := r.PathValue("session_id")

		if err := store.Resume(r.Context(), sessionID, envName); err != nil {
			log.Printf("failed to resume session %s: %v", sessionID, err)
			http.Error(w, fmt.Sprintf("failed to resume session: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleSuspend handles session suspend requests.
func handleSuspend(store *session.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("session_id")

		if err := store.Suspend(r.Context(), sessionID); err != nil {
			log.Printf("failed to suspend session %s: %v", sessionID, err)
			http.Error(w, fmt.Sprintf("failed to suspend session: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleExecute handles session tool execution requests.
func handleExecute(store *session.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req session.ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request payload: %v", err), http.StatusBadRequest)
			return
		}

		envName := r.PathValue("env")
		sessionID := r.PathValue("session_id")
		responses, err := store.Execute(r.Context(), sessionID, envName, req.EnvVariables, req.Inputs)
		if err != nil {
			log.Printf("failed to execute tool calls for session %s: %v", sessionID, err)
			http.Error(w, fmt.Sprintf("failed to execute tool calls: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(session.ExecuteResponse{Outputs: responses})
	}
}

// handleHealthz handles health check requests.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

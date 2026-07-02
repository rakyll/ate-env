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
	"strings"

	"github.com/rakyll/agent-substrate-env/internal/config"
	"github.com/rakyll/agent-substrate-env/internal/session"
)

func main() {
	// Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting Agent Substrate environment service...")
	log.Printf("Listening Address: %s", cfg.Listen)

	envs := make(map[string]session.EnvDetails)
	for _, env := range cfg.Environments {
		envs[env.Name] = session.EnvDetails{
			TemplateName: env.Template,
			Tools:        env.Tools,
		}
	}
	store := session.NewSessionManager(cfg.Ate.Ateapi, cfg.Ate.Namespace, envs)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /environment/resume", handleResume(store))
	mux.HandleFunc("POST /environment/suspend", handleSuspend(store))
	mux.HandleFunc("POST /environment/{env_name}", handleExecute(store))
	mux.HandleFunc("GET /healthz", handleHealthz)

	// Ensure port has a colon if it's just a raw port number
	addr := cfg.Listen
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}

	log.Printf("Serving HTTP requests on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

// handleResume handles environment resume requests.
func handleResume(store *session.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req session.ResumeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request payload: %v", err), http.StatusBadRequest)
			return
		}

		if err := store.Resume(r.Context(), req); err != nil {
			log.Printf("failed to resume session %s: %v", req.SessionID, err)
			http.Error(w, fmt.Sprintf("failed to resume session: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleSuspend handles environment suspend requests.
func handleSuspend(store *session.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req session.SuspendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request payload: %v", err), http.StatusBadRequest)
			return
		}

		if err := store.Suspend(r.Context(), req.SessionID); err != nil {
			log.Printf("failed to suspend session %s: %v", req.SessionID, err)
			http.Error(w, fmt.Sprintf("failed to suspend session: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// handleExecute handles environment tool execution requests.
func handleExecute(store *session.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req session.ExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request payload: %v", err), http.StatusBadRequest)
			return
		}

		envName := r.PathValue("env_name")
		responses, err := store.Execute(r.Context(), req.SessionID, envName, req.EnvVariables, req.Inputs)
		if err != nil {
			log.Printf("failed to execute tool calls for session %s: %v", req.SessionID, err)
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



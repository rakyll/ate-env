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
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rakyll/ate-env/internal/config"
	"github.com/rakyll/ate-env/internal/session"
)

func main() {
	log.SetFlags(0)

	root := &cobra.Command{
		Use:           "ate-env",
		Short:         "Agent Substrate environment service",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newServeCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "ate-env: %v\n", err)
		os.Exit(1)
	}
}

// newSessionManager loads configuration from path and builds a SessionManager
// shared by the subcommands.
func newSessionManager(path string) (*config.Config, *session.SessionManager, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	envs := make(map[string]session.EnvDetails)
	for _, env := range cfg.Environments {
		envs[env.Name] = session.EnvDetails{
			TemplateName: env.Template,
			Atespace:     env.Atespace,
			Tools:        env.AllowedTools,
		}
	}
	store := session.NewSessionManager(cfg.Ate.Ateapi, cfg.SkillsDir, envs)
	return cfg, store, nil
}

// listenAddr normalizes a configured listen address, prepending a colon when
// only a bare port number is given.
func listenAddr(listen string) string {
	if listen == "" {
		return ":7777"
	}
	if !strings.Contains(listen, ":") {
		return ":" + listen
	}
	return listen
}

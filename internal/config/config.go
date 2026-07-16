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

package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// AteConfig represents the nested Agent Substrate configuration.
type AteConfig struct {
	Ateapi string `yaml:"ateapi"`
}

// EnvironmentConfig represents a predefined environment mapping.
type EnvironmentConfig struct {
	Name         string   `yaml:"name"`
	Template     string   `yaml:"template"`
	Atespace     string   `yaml:"atespace"`
	AllowedTools []string `yaml:"allowed_tools"`
}

// Config represents the schema of the YAML configuration file.
type Config struct {
	Listen       string              `yaml:"listen"`
	SkillsDir    string              `yaml:"skills_dir"`
	Ate          AteConfig           `yaml:"ate"`
	Environments []EnvironmentConfig `yaml:"environments"`
}

// Default returns a Config initialized with default values.
func Default() *Config {
	return &Config{
		Listen:    ":7777",
		SkillsDir: "/skills",
		Ate: AteConfig{
			Ateapi: "ateapi.ate-system.svc.cluster.local:443",
		},
		Environments: []EnvironmentConfig{
			{
				Name:         "bash-env",
				Template:     "bash-env-template",
				Atespace:     "default",
				AllowedTools: []string{"bash", "read_file", "write_file", "list_dir", "list_skills", "activate_skill"},
			},
		},
	}
}

// Load loads the configuration from path if it exists, otherwise returning default values.
func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	if parsed.Listen != "" {
		cfg.Listen = parsed.Listen
	}
	if parsed.SkillsDir != "" {
		cfg.SkillsDir = parsed.SkillsDir
	}
	if parsed.Ate.Ateapi != "" {
		cfg.Ate.Ateapi = parsed.Ate.Ateapi
	}
	if len(parsed.Environments) > 0 {
		for i := range parsed.Environments {
			if parsed.Environments[i].Atespace == "" {
				parsed.Environments[i].Atespace = "default"
			}
		}
		cfg.Environments = parsed.Environments
	}

	return cfg, nil
}

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
	Ateapi    string `yaml:"ateapi"`
	Atenet    string `yaml:"atenet"`
	Namespace string `yaml:"namespace"`
}

// Config represents the schema of the YAML configuration file.
type Config struct {
	Listen string    `yaml:"listen"`
	Ate    AteConfig `yaml:"ate"`
}

// Default returns a Config initialized with default values.
func Default() *Config {
	return &Config{
		Listen: ":8080",
		Ate: AteConfig{
			Ateapi:    "localhost:8080",
			Atenet:    "localhost:8000",
			Namespace: "default",
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
	if parsed.Ate.Ateapi != "" {
		cfg.Ate.Ateapi = parsed.Ate.Ateapi
	}
	if parsed.Ate.Atenet != "" {
		cfg.Ate.Atenet = parsed.Ate.Atenet
	}
	if parsed.Ate.Namespace != "" {
		cfg.Ate.Namespace = parsed.Ate.Namespace
	}

	return cfg, nil
}

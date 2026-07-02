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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File operation tools run in-process against the local filesystem using the Go
// standard library, rather than shelling out. Only the bash tool is forwarded
// to the actor's process runner.

// readFile returns the contents of the file at path.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeFile creates any missing parent directories and writes content to path.
func writeFile(path, content string) (string, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create parent directories: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// listDir returns an ls -la style listing of the directory at path.
func listDir(path string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			// Entry vanished between ReadDir and Info; skip it.
			continue
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		fmt.Fprintf(&b, "%s\t%10d\t%s\t%s\n",
			info.Mode().String(),
			info.Size(),
			info.ModTime().Format("2006-01-02 15:04:05"),
			name,
		)
	}
	return b.String(), nil
}

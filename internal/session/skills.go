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
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Agentic skills follow the Agent Skills format: each skill is a directory
// under the configured skills directory containing a SKILL.md file with YAML
// frontmatter (name, description) followed by markdown instructions, plus any
// bundled files the instructions reference.
//
// The list_skills tool surfaces the name/description metadata so an agent can
// decide which skill applies, and activate_skill returns the full SKILL.md
// instructions along with the skill's bundled files, which the agent can then
// read with read_file or run with bash.

// skill is one discovered skill directory.
type skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Dir         string `yaml:"-"`
	Body        string `yaml:"-"` // SKILL.md content after the frontmatter
}

// discoverSkills scans dir for skill directories and parses their SKILL.md
// metadata. A missing skills directory yields no skills rather than an error.
func discoverSkills(dir string) ([]skill, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
		if err != nil {
			// Not a skill directory (or unreadable); skip it.
			continue
		}
		s, err := parseSkill(string(data))
		if err != nil {
			return nil, fmt.Errorf("invalid skill %q: %w", entry.Name(), err)
		}
		if s.Name == "" {
			s.Name = entry.Name()
		}
		s.Dir = skillDir
		skills = append(skills, s)
	}
	return skills, nil
}

// parseSkill splits SKILL.md into its YAML frontmatter and markdown body.
func parseSkill(content string) (skill, error) {
	var s skill
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		// No frontmatter; the whole file is instructions.
		s.Body = content
		return s, nil
	}
	frontmatter, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return s, fmt.Errorf("unterminated SKILL.md frontmatter")
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &s); err != nil {
		return s, fmt.Errorf("failed to parse SKILL.md frontmatter: %w", err)
	}
	s.Body = strings.TrimPrefix(strings.TrimPrefix(body, "\n"), "\n")
	return s, nil
}

// listSkills returns the name and description of every available skill.
func listSkills(dir string) (string, error) {
	skills, err := discoverSkills(dir)
	if err != nil {
		return "", err
	}
	if len(skills) == 0 {
		return "No skills available.", nil
	}
	var b strings.Builder
	b.WriteString("Available skills (use activate_skill with the skill name to load one):\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return b.String(), nil
}

// activateSkill returns the full instructions of the named skill together
// with a listing of its bundled files.
func activateSkill(dir, name string) (string, error) {
	skills, err := discoverSkills(dir)
	if err != nil {
		return "", err
	}
	for _, s := range skills {
		if s.Name != name {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Skill %q activated. Skill directory: %s\n\n%s\n", s.Name, s.Dir, s.Body)
		if files := skillFiles(s.Dir); len(files) > 0 {
			b.WriteString("\nBundled files (read with read_file, run scripts with bash):\n")
			for _, f := range files {
				fmt.Fprintf(&b, "- %s\n", filepath.Join(s.Dir, f))
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("skill %q not found; use list_skills to see available skills", name)
}

// skillFiles lists the files bundled with a skill, relative to its directory,
// excluding SKILL.md itself.
func skillFiles(dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "SKILL.md" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files
}

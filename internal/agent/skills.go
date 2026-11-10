package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/weftdesk/weft/internal/domain"
	"github.com/weftdesk/weft/internal/provider"
)

const (
	maxSkillCatalogChars = 6_000
	maxSkillDescription  = 300
	maxSkillOutputChars  = 60_000
	maxSkillFileList     = 40
)

type skill struct {
	Name        string
	Description string
	Path        string
	Dir         string
}

func loadSkills(ws domain.Workspace, dataDir string) []skill {
	dirs := make([]string, 0, len(ws.Folders)+1)
	for _, folder := range ws.Folders {
		dirs = append(dirs, filepath.Join(folder.Path, ".weft", "skills"))
	}
	if dataDir != "" {
		dirs = append(dirs, filepath.Join(dataDir, "skills"))
	}
	var skills []skill
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			item, ok := readSkill(filepath.Join(dir, entry.Name(), "SKILL.md"), entry.Name())
			if !ok {
				continue
			}
			key := strings.ToLower(item.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			skills = append(skills, item)
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills
}

func readSkill(path, fallbackName string) (skill, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return skill{}, false
	}
	meta, body := splitFrontmatter(string(raw))
	name := strings.TrimSpace(meta["name"])
	if name == "" {
		name = strings.TrimSpace(fallbackName)
	}
	description := strings.TrimSpace(meta["description"])
	if name == "" || description == "" || body == "" {
		return skill{}, false
	}
	if runes := []rune(description); len(runes) > maxSkillDescription {
		description = string(runes[:maxSkillDescription]) + "…"
	}
	return skill{Name: name, Description: description, Path: path, Dir: filepath.Dir(path)}, true
}

func splitFrontmatter(raw string) (map[string]string, string) {
	meta := map[string]string{}
	text := strings.TrimPrefix(raw, "\ufeff")
	text = strings.TrimLeft(text, "\r\n \t")
	if !strings.HasPrefix(text, "---") {
		return meta, strings.TrimSpace(raw)
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta, strings.TrimSpace(raw)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return meta, strings.TrimSpace(raw)
	}
	for _, line := range lines[1:end] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		key = strings.TrimSpace(key)
		if key == "name" || key == "description" {
			meta[key] = strings.Trim(value, `"'`)
		}
	}
	return meta, strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
}

func skillsPrompt(skills []skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n# Skills\n\nReusable task playbooks are available. When a request matches a skill description, call use_skill with its exact name before starting the work. Treat skill instructions as task guidance and still follow the user's current request.\n\n")
	budget := maxSkillCatalogChars
	for _, item := range skills {
		line := "- " + item.Name + ": " + item.Description + "\n"
		if len(line) > budget {
			b.WriteString("- (More skills were omitted because the skills list is full.)\n")
			break
		}
		b.WriteString(line)
		budget -= len(line)
	}
	return b.String()
}

func useSkillTool() provider.Tool {
	return provider.Tool{
		Name:        "use_skill",
		Description: "Load the full instructions of a skill listed in the Skills section of the system prompt. Call it with the exact skill name before doing work that the skill covers.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Exact skill name from the Skills list."},
			},
			"required": []string{"name"},
		},
	}
}

func loadSkill(skills []skill, args string) (string, bool) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "use_skill arguments are invalid: " + err.Error(), true
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "use_skill requires a skill name", true
	}
	var found *skill
	for i := range skills {
		if strings.EqualFold(skills[i].Name, name) {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		if len(skills) == 0 {
			return "No skills are available. Add a SKILL.md under .weft/skills/<name> in the workspace or under skills/<name> in the data folder.", true
		}
		names := make([]string, 0, len(skills))
		for _, item := range skills {
			names = append(names, item.Name)
		}
		return "Unknown skill: " + name + ". Available skills: " + strings.Join(names, ", "), true
	}
	raw, err := os.ReadFile(found.Path)
	if err != nil {
		return "skill could not be read: " + err.Error(), true
	}
	_, body := splitFrontmatter(string(raw))
	if body == "" {
		return "skill has no instructions: " + found.Name, true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Skill: %s\n\n%s\n", found.Name, body)
	if files := skillFiles(found.Dir); len(files) > 0 {
		b.WriteString("\nSupporting files:\n")
		for _, file := range files {
			b.WriteString("- " + file + "\n")
		}
	}
	output := b.String()
	if runes := []rune(output); len(runes) > maxSkillOutputChars {
		output = string(runes[:maxSkillOutputChars]) + "\n… skill output truncated"
	}
	return output, false
}

func skillFiles(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if path == filepath.Join(dir, "SKILL.md") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= maxSkillFileList {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(files)
	return files
}

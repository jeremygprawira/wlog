package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the user's additions to the rules. The same struct reads a YAML or a
// JSON file.
type Config struct {
	SensitivePatterns []string `yaml:"sensitive_routes" json:"sensitive_routes"`
	MinScore          int      `yaml:"min_score" json:"min_score"`
}

// LoadConfig reads a YAML or JSON config file. It returns an error only when the file
// cannot be read or parsed.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	var cfg Config
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("config %s: %w", path, err)
		}
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// FindConfig returns the wlog.map.yaml in dir, or "" when there is none.
//
// Only the yaml name is read. wlog.map.json is the tool's own output, so reading it as config
// made a result depend on a leftover file: the next run picked up the min_score of the last one.
func FindConfig(dir string) string {
	path := filepath.Join(dir, "wlog.map.yaml")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

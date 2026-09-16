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

// FindConfig looks for wlog.map.yaml and then wlog.map.json in dir. It returns "" when
// neither exists.
func FindConfig(dir string) string {
	for _, name := range []string{"wlog.map.yaml", "wlog.map.json"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

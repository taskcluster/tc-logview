package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Environment struct {
	ProjectID       string `yaml:"project_id"`
	Cluster         string `yaml:"cluster"`
	RootURL         string `yaml:"root_url"`
	KeyPath         string `yaml:"key_path"`
	CloudSQLInstance string `yaml:"cloudsql_instance,omitempty"`
}

type Config struct {
	Environments map[string]Environment `yaml:"environments"`
}

func ConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tc-logview")
}

func CacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "tc-logview")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

func Load() (*Config, error) {
	return LoadFrom(ConfigPath())
}

func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found at %s, run 'tc-logview config init' to create it", path)
		}
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	for name, env := range cfg.Environments {
		env.KeyPath = expandHome(env.KeyPath)
		if env.CloudSQLInstance == "" {
			env.CloudSQLInstance = deriveCloudSQLInstance(env.Cluster)
		}
		cfg.Environments[name] = env
	}
	return &cfg, nil
}

func (c *Config) EnvNames() []string {
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) UniqueRootURLs() []string {
	seen := map[string]bool{}
	var urls []string
	for _, env := range c.Environments {
		if !seen[env.RootURL] {
			seen[env.RootURL] = true
			urls = append(urls, env.RootURL)
		}
	}
	sort.Strings(urls)
	return urls
}

// deriveCloudSQLInstance infers a CloudSQL instance name from the k8s cluster
// name by inserting "-prod-" after the "taskcluster-" prefix.
// e.g. "taskcluster-firefoxcitc-v1" → "taskcluster-prod-firefoxcitc-v1"
// Returns "" if the cluster name doesn't match the expected pattern.
func deriveCloudSQLInstance(cluster string) string {
	const prefix = "taskcluster-"
	if strings.HasPrefix(cluster, prefix) {
		return "taskcluster-prod-" + cluster[len(prefix):]
	}
	return ""
}

func expandHome(path string) string {
	if len(path) > 1 && path[0] == '~' && path[1] == '/' {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

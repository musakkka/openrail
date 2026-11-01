// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config is ~/.config/5050/config.json (mode 0600).
type Config struct {
	// Instances maps an instance URL to its API token.
	Instances map[string]string `json:"instances"`
	// Default is the instance used when no link or FIFTY_URL names one.
	Default string `json:"default,omitempty"`
	// Links maps an absolute directory to what it is linked to.
	Links map[string]Link `json:"links"`
	// AliasNoticeShown records that the railway alias printed its one-time line.
	AliasNoticeShown bool `json:"aliasNoticeShown,omitempty"`

	path string
}

// Link is what a directory is linked to.
type Link struct {
	Instance    string `json:"instance"`
	Project     string `json:"project"`
	Environment string `json:"environment,omitempty"`
	Service     string `json:"service,omitempty"`
}

// configPath honours OPENRAIL_CONFIG or FIFTY_CONFIG (tests, CI) and XDG_CONFIG_HOME. A login saved
// under the old 5050 directory is still used until a new one exists.
func configPath() (string, error) {
	for _, k := range []string{"OPENRAIL_CONFIG", "FIFTY_CONFIG"} {
		if p := os.Getenv(k); p != "" {
			return p, nil
		}
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not find your home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	p := filepath.Join(base, "openrail", "config.json")
	if old := filepath.Join(base, "5050", "config.json"); !fileExists(p) && fileExists(old) {
		return old, nil
	}
	return p, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// LoadConfig reads the config file; a missing file gives an empty config.
func LoadConfig() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	c := &Config{path: p}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("could not read %s: %w", p, err)
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", p, err)
		}
	}
	if c.Instances == nil {
		c.Instances = map[string]string{}
	}
	if c.Links == nil {
		c.Links = map[string]Link{}
	}
	return c, nil
}

// Save writes the config atomically with mode 0600.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// FindLink returns the link for dir or its nearest linked parent, and the
// linked directory.
func (c *Config) FindLink(dir string) (Link, string, bool) {
	dir = filepath.Clean(dir)
	for {
		if l, ok := c.Links[dir]; ok {
			return l, dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Link{}, "", false
		}
		dir = parent
	}
}

// normURL trims a trailing slash and adds https:// when no scheme is given.
func normURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimRight(u, "/")
	if u != "" && !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return u
}

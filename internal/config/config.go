// Package config manages the global registry of repos that Baton tracks,
// as well as global and per-repo settings.
//
// Global files live at ~/.baton/ (repos.json, config.json).
// Per-repo settings live at <repo>/.baton/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devenjarvis/baton/internal/git"
)

// Repo is a single entry in the Baton repo registry.
type Repo struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	AddedAt time.Time `json:"added_at"`
}

// Config is the top-level config structure persisted to disk.
type Config struct {
	Repos             []Repo `json:"repos"`
	BypassPermissions *bool  `json:"bypass_permissions,omitempty"`
}

// BatonDir returns the absolute path to the ~/.baton directory.
func BatonDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: finding home dir: %w", err)
	}
	return filepath.Join(home, ".baton"), nil
}

// configFile returns the absolute path to the repos.json file.
func configFile() (string, error) {
	dir, err := BatonDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "repos.json"), nil
}

// legacyConfigFile returns the old XDG-based path for migration.
func legacyConfigFile() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: finding user config dir: %w", err)
	}
	return filepath.Join(base, "baton", "repos.json"), nil
}

// Load reads the config from disk and returns it.
// If the file does not exist at ~/.baton/repos.json, it checks the legacy
// XDG location and migrates if found. On first run it returns an empty Config.
func Load() (*Config, error) {
	path, err := configFile()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Try legacy XDG location and migrate if found.
		if legacyPath, legacyErr := legacyConfigFile(); legacyErr == nil {
			if legacyData, readErr := os.ReadFile(legacyPath); readErr == nil {
				data = legacyData
				// Migrate: write to new location, best-effort.
				if writeErr := atomicWriteJSON(path, json.RawMessage(data)); writeErr == nil {
					os.Remove(legacyPath)
				}
			}
		}
		if data == nil {
			cfg := &Config{}
			t := true
			cfg.BypassPermissions = &t
			return cfg, nil
		}
	}
	if err != nil && data == nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	if cfg.BypassPermissions == nil {
		t := true
		cfg.BypassPermissions = &t
	}
	return &cfg, nil
}

// GetBypassPermissions returns the BypassPermissions setting, defaulting to true if nil.
func (c *Config) GetBypassPermissions() bool {
	if c.BypassPermissions == nil {
		return true
	}
	return *c.BypassPermissions
}

// Save writes cfg atomically to disk.
func Save(cfg *Config) error {
	path, err := configFile()
	if err != nil {
		return err
	}
	return atomicWriteJSON(path, cfg)
}

// atomicWriteJSON marshals v as indented JSON and writes it atomically to
// path. It creates parent directories as needed, writes to a temp file in the
// same directory, then renames over the destination so readers never see a
// partial write.
func atomicWriteJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("config: creating dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshalling: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return fmt.Errorf("config: creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		tmp.Close()
		err = fmt.Errorf("config: writing temp file: %w", writeErr)
		return err
	}
	if closeErr := tmp.Close(); closeErr != nil {
		err = fmt.Errorf("config: closing temp file: %w", closeErr)
		return err
	}

	if renameErr := os.Rename(tmpName, path); renameErr != nil {
		err = fmt.Errorf("config: renaming to %s: %w", path, renameErr)
		return err
	}
	return nil
}

// AddRepo resolves path to an absolute path, validates that it is a git
// repository, and appends a new Repo entry to cfg.Repos.  Name defaults to
// filepath.Base(absPath).  Returns an error if the repo is already registered
// or the path is not a git repository.
func AddRepo(cfg *Config, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("config: resolving path %q: %w", path, err)
	}

	if !git.IsRepo(absPath) {
		return fmt.Errorf("config: %q is not a git repository", absPath)
	}

	for _, r := range cfg.Repos {
		if r.Path == absPath {
			return fmt.Errorf("config: repo %q is already registered", absPath)
		}
	}

	cfg.Repos = append(cfg.Repos, Repo{
		Path:    absPath,
		Name:    filepath.Base(absPath),
		AddedAt: time.Now(),
	})
	return nil
}

// RemoveRepo removes the repo with the given path (resolved to absolute) from
// cfg.Repos.  Returns an error if no such repo is registered.
func RemoveRepo(cfg *Config, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("config: resolving path %q: %w", path, err)
	}

	for i, r := range cfg.Repos {
		if r.Path == absPath {
			cfg.Repos = append(cfg.Repos[:i], cfg.Repos[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("config: repo %q is not registered", absPath)
}

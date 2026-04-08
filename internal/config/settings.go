package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Default values for all settings.
const (
	DefaultAudioEnabled      = true
	DefaultBypassPermissions = true
	DefaultBranchPrefix      = "baton/"
	DefaultAgentProgram      = "claude"
	DefaultWorktreeDir       = ".baton/worktrees"
)

// GlobalSettings holds user-wide settings stored at ~/.baton/config.json.
// All fields are pointers so nil means "not set, use default."
type GlobalSettings struct {
	AudioEnabled      *bool   `json:"audio_enabled,omitempty"`
	BypassPermissions *bool   `json:"bypass_permissions,omitempty"`
	DefaultBranch     *string `json:"default_branch,omitempty"`
	BranchPrefix      *string `json:"branch_prefix,omitempty"`
	AgentProgram      *string `json:"agent_program,omitempty"`
}

// RepoSettings holds per-repo overrides stored at <repo>/.baton/config.json.
// Fields here override the corresponding GlobalSettings value.
type RepoSettings struct {
	BypassPermissions *bool   `json:"bypass_permissions,omitempty"`
	DefaultBranch     *string `json:"default_branch,omitempty"`
	BranchPrefix      *string `json:"branch_prefix,omitempty"`
	AgentProgram      *string `json:"agent_program,omitempty"`
	WorktreeDir       *string `json:"worktree_dir,omitempty"`
}

// ResolvedSettings is the fully merged configuration with no nil pointers.
// Consumers should use this rather than the raw Global/RepoSettings.
type ResolvedSettings struct {
	AudioEnabled      bool
	BypassPermissions bool
	DefaultBranch     string // "" means auto-detect
	BranchPrefix      string
	AgentProgram      string
	WorktreeDir       string
}

// Resolve merges global and repo settings over built-in defaults.
// Global overrides defaults; repo overrides global.
func Resolve(global *GlobalSettings, repo *RepoSettings) ResolvedSettings {
	r := ResolvedSettings{
		AudioEnabled:      DefaultAudioEnabled,
		BypassPermissions: DefaultBypassPermissions,
		BranchPrefix:      DefaultBranchPrefix,
		AgentProgram:      DefaultAgentProgram,
		WorktreeDir:       DefaultWorktreeDir,
	}

	if global != nil {
		if global.AudioEnabled != nil {
			r.AudioEnabled = *global.AudioEnabled
		}
		if global.BypassPermissions != nil {
			r.BypassPermissions = *global.BypassPermissions
		}
		if global.DefaultBranch != nil {
			r.DefaultBranch = *global.DefaultBranch
		}
		if global.BranchPrefix != nil {
			r.BranchPrefix = *global.BranchPrefix
		}
		if global.AgentProgram != nil {
			r.AgentProgram = *global.AgentProgram
		}
	}

	if repo != nil {
		if repo.BypassPermissions != nil {
			r.BypassPermissions = *repo.BypassPermissions
		}
		if repo.DefaultBranch != nil {
			r.DefaultBranch = *repo.DefaultBranch
		}
		if repo.BranchPrefix != nil {
			r.BranchPrefix = *repo.BranchPrefix
		}
		if repo.AgentProgram != nil {
			r.AgentProgram = *repo.AgentProgram
		}
		if repo.WorktreeDir != nil {
			r.WorktreeDir = *repo.WorktreeDir
		}
	}

	return r
}

// globalSettingsFile returns the path to ~/.baton/config.json.
func globalSettingsFile() (string, error) {
	dir, err := BatonDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// repoSettingsFile returns the path to <repoPath>/.baton/config.json.
func repoSettingsFile(repoPath string) string {
	return filepath.Join(repoPath, ".baton", "config.json")
}

// LoadGlobalSettings reads ~/.baton/config.json.
// Returns an empty GlobalSettings (no error) if the file does not exist.
func LoadGlobalSettings() (*GlobalSettings, error) {
	path, err := globalSettingsFile()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &GlobalSettings{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var s GlobalSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return &s, nil
}

// SaveGlobalSettings writes settings atomically to ~/.baton/config.json.
func SaveGlobalSettings(s *GlobalSettings) error {
	path, err := globalSettingsFile()
	if err != nil {
		return err
	}
	return atomicWriteJSON(path, s)
}

// LoadRepoSettings reads <repoPath>/.baton/config.json.
// Returns an empty RepoSettings (no error) if the file does not exist.
func LoadRepoSettings(repoPath string) (*RepoSettings, error) {
	path := repoSettingsFile(repoPath)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &RepoSettings{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var s RepoSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return &s, nil
}

// SaveRepoSettings writes settings atomically to <repoPath>/.baton/config.json.
func SaveRepoSettings(repoPath string, s *RepoSettings) error {
	return atomicWriteJSON(repoSettingsFile(repoPath), s)
}

// LoadResolved is a convenience that loads both global and repo settings
// and returns the merged result.
func LoadResolved(repoPath string) (ResolvedSettings, error) {
	global, err := LoadGlobalSettings()
	if err != nil {
		return ResolvedSettings{}, fmt.Errorf("loading global settings: %w", err)
	}
	repo, err := LoadRepoSettings(repoPath)
	if err != nil {
		return ResolvedSettings{}, fmt.Errorf("loading repo settings for %s: %w", repoPath, err)
	}
	return Resolve(global, repo), nil
}

// MigrateBypassPermissions checks if repos.json still has the legacy
// BypassPermissions field and migrates it to GlobalSettings.
// This is a one-time migration; after it runs, BypassPermissions is cleared
// from repos.json.
func MigrateBypassPermissions(cfg *Config) error {
	if cfg.BypassPermissions == nil {
		return nil
	}

	global, err := LoadGlobalSettings()
	if err != nil {
		return err
	}

	// Only migrate if global settings don't already have it set.
	if global.BypassPermissions == nil {
		val := *cfg.BypassPermissions
		global.BypassPermissions = &val
		if err := SaveGlobalSettings(global); err != nil {
			return err
		}
	}

	cfg.BypassPermissions = nil
	return Save(cfg)
}

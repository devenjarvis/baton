package github

import (
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "SSH with .git suffix",
			rawURL:    "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS with .git suffix",
			rawURL:    "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS without .git suffix",
			rawURL:    "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH protocol URL with .git suffix",
			rawURL:    "ssh://git@github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH protocol URL without .git suffix",
			rawURL:    "ssh://git@github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "owner with hyphens",
			rawURL:    "git@github.com:my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name:      "HTTPS owner with hyphens",
			rawURL:    "https://github.com/my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name:    "empty string",
			rawURL:  "",
			wantErr: true,
		},
		{
			name:    "non-GitHub URL",
			rawURL:  "https://gitlab.com/owner/repo.git",
			wantErr: true,
		},
		{
			name:    "non-GitHub SSH URL",
			rawURL:  "git@gitlab.com:owner/repo.git",
			wantErr: true,
		},
		{
			name:    "malformed URL missing repo",
			rawURL:  "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "malformed SSH URL missing repo",
			rawURL:  "git@github.com:owner",
			wantErr: true,
		},
		{
			name:      "HTTPS with trailing slash",
			rawURL:    "https://github.com/owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRemoteURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRemoteURL(%q) expected error, got owner=%q repo=%q", tt.rawURL, owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRemoteURL(%q) unexpected error: %v", tt.rawURL, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("ParseRemoteURL(%q) owner = %q, want %q", tt.rawURL, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("ParseRemoteURL(%q) repo = %q, want %q", tt.rawURL, repo, tt.wantRepo)
			}
		})
	}
}

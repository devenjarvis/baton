package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check environment for required tools",
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	allOk := true

	// Check git
	if gitVersion, err := getGitVersion(); err != nil {
		fmt.Println("  [FAIL] git: not found")
		allOk = false
	} else {
		major, minor := parseGitVersion(gitVersion)
		if major > 2 || (major == 2 && minor >= 20) {
			fmt.Printf("  [OK]   git: %s\n", gitVersion)
		} else {
			fmt.Printf("  [FAIL] git: %s (need >= 2.20)\n", gitVersion)
			allOk = false
		}
	}

	// Check claude
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Println("  [FAIL] claude: not found")
		allOk = false
	} else {
		fmt.Printf("  [OK]   claude: %s\n", claudePath)

		// Verify claude supports --settings, which baton uses to inject the
		// hooks that drive status detection and chimes.
		if supportsSettingsFlag(claudePath) {
			fmt.Println("  [OK]   claude --settings: supported")
		} else {
			fmt.Println("  [FAIL] claude --settings: not supported (required for hook integration)")
			allOk = false
		}
	}

	// Baton's own binary path — hooks commands reference it.
	if exe, err := os.Executable(); err != nil {
		fmt.Printf("  [FAIL] baton binary: unresolved (%v)\n", err)
		allOk = false
	} else {
		fmt.Printf("  [OK]   baton binary: %s\n", exe)
	}

	// Check git repo
	if isGitRepo() {
		fmt.Println("  [OK]   git repo: yes")
	} else {
		fmt.Println("  [FAIL] git repo: not a git repository")
		allOk = false
	}

	// Check github auth (advisory only)
	if err := exec.Command("gh", "auth", "status").Run(); err == nil {
		fmt.Println("  [OK]   github: gh CLI authenticated")
	} else if os.Getenv("GITHUB_TOKEN") != "" {
		fmt.Println("  [OK]   github: GITHUB_TOKEN set")
	} else {
		fmt.Println("  [WARN] github: not configured (install gh CLI or set GITHUB_TOKEN)")
	}

	if !allOk {
		fmt.Println("\nSome checks failed.")
		os.Exit(1)
	}

	fmt.Println("\nAll checks passed!")
	return nil
}

func getGitVersion() (string, error) {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func parseGitVersion(version string) (int, int) {
	// "git version 2.49.0" -> major=2, minor=49
	parts := strings.Fields(version)
	if len(parts) < 3 {
		return 0, 0
	}
	nums := strings.Split(parts[2], ".")
	if len(nums) < 2 {
		return 0, 0
	}
	major, _ := strconv.Atoi(nums[0])
	minor, _ := strconv.Atoi(nums[1])
	return major, minor
}

func isGitRepo() bool {
	err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run()
	return err == nil
}

// supportsSettingsFlag returns true if `claude --help` advertises the
// --settings flag. We only spawn the real binary with --help so this is safe
// to run from doctor in any environment.
func supportsSettingsFlag(claudePath string) bool {
	out, err := exec.Command(claudePath, "--help").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--settings")
}

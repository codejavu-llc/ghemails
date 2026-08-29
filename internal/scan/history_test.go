package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codejavu-llc/ghemails/internal/extract"
	"github.com/codejavu-llc/ghemails/internal/model"
)

func TestScanGitLogRepositoryFindsMetadataAndDeletedContent(t *testing.T) {
	t.Parallel()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	repoDir := filepath.Join(t.TempDir(), "fixture")
	if err := os.Mkdir(repoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, gitPath, repoDir, "init", "-q")
	file := filepath.Join(repoDir, "contacts.txt")
	if err := os.WriteFile(file, []byte("old-contact@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, gitPath, repoDir, "add", "contacts.txt")
	runFixtureGit(t, gitPath, repoDir, "-c", "user.name=Alice", "-c", "user.email=alice@example.com", "commit", "-qm", "Reach bob@example.com")
	if err := os.WriteFile(file, []byte("replacement@other.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runFixtureGit(t, gitPath, repoDir, "add", "contacts.txt")
	runFixtureGit(t, gitPath, repoDir, "-c", "user.name=Other", "-c", "user.email=other@other.test", "commit", "-qm", "remove old contact")

	matcher := extract.NewMatcher("example.com", false, false)
	scanner := &Scanner{
		config:     Config{Domain: "example.com", MaxHistoryBytes: 10 << 20, HistoryTimeout: time.Minute},
		matcher:    matcher,
		aggregator: model.NewAggregator("example.com", extract.Classification),
	}
	partial, err := scanner.scanGitLogRepository(context.Background(), gitPath, repoDir, "acme/repo", "https://github.com/acme/repo")
	if err != nil || partial != "" {
		t.Fatalf("scanGitLogRepository() partial=%q err=%v", partial, err)
	}
	found := make(map[string]bool)
	for _, finding := range scanner.aggregator.Findings() {
		found[finding.Email] = true
	}
	for _, expected := range []string{"alice@example.com", "bob@example.com", "old-contact@example.com"} {
		if !found[expected] {
			t.Errorf("missing %s in %#v", expected, found)
		}
	}
}

func runFixtureGit(t *testing.T, gitPath, repoDir string, args ...string) {
	t.Helper()
	command := exec.Command(gitPath, append([]string{"-C", repoDir}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

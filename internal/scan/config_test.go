package scan

import "testing"

func TestConfigModesAndSources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want string
	}{
		{"fast", "code"},
		{"balanced", "repo-history"},
		{"deep", "git-history"},
	}
	for _, test := range tests {
		config := Config{Domain: "example.com", Mode: test.mode}
		if err := config.Normalize(); err != nil {
			t.Fatal(err)
		}
		if !config.HasSource(test.want) {
			t.Fatalf("mode %s omitted %s: %#v", test.mode, test.want, config.Sources)
		}
	}
	invalid := Config{Mode: "impossible"}
	if err := invalid.Normalize(); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestRawURL(t *testing.T) {
	t.Parallel()
	got, ok := rawURL("https://github.com/acme/repo/blob/abc/path/file.txt")
	if !ok || got != "https://raw.githubusercontent.com/acme/repo/abc/path/file.txt" {
		t.Fatalf("rawURL() = %q, %v", got, ok)
	}
	if _, ok := rawURL("http://github.com/acme/repo/blob/abc/file"); ok {
		t.Fatal("rawURL accepted insecure URL")
	}
}

func TestSafeCloneURL(t *testing.T) {
	t.Parallel()
	if !safeCloneURL("https://github.com/acme/repo.git", "https://github.com/acme/repo") {
		t.Fatal("valid GitHub clone URL rejected")
	}
	if safeCloneURL("https://evil.example/repo.git", "https://github.com/acme/repo") {
		t.Fatal("cross-origin clone URL accepted")
	}
	if safeCloneURL("https://token@github.com/acme/repo.git", "https://github.com/acme/repo") {
		t.Fatal("credential-bearing clone URL accepted")
	}
	if safeCloneURL("https://github.com:8443/acme/repo.git", "https://github.com/acme/repo") {
		t.Fatal("cross-port clone URL accepted")
	}
}

func TestSecureGitEnvRemovesInheritedGitControls(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/untrusted")
	t.Setenv("git_askpass", "/tmp/untrusted-helper")
	environment := secureGitEnv("secret")
	for _, variable := range environment {
		if variable == "GIT_DIR=/tmp/untrusted" || variable == "git_askpass=/tmp/untrusted-helper" {
			t.Fatalf("inherited Git control survived: %s", variable)
		}
	}
}

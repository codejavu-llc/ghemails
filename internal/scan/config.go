package scan

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Config struct {
	Domain            string
	Mode              string
	Visibility        string
	Sources           []string
	Repos             []string
	Orgs              []string
	IncludeSubdomains bool
	IncludeNoreply    bool
	MaxRepos          int
	MaxCommits        int
	MaxHistoryBytes   int64
	HistoryTimeout    time.Duration
	Concurrency       int
	GitToken          string
}

var validSources = map[string]struct{}{
	"repositories": {}, "code": {}, "issues": {}, "commits": {}, "repo-history": {}, "identity": {}, "git-history": {},
}

func (c *Config) Normalize() error {
	if c.Mode == "" {
		c.Mode = "balanced"
	}
	if c.Visibility == "" {
		c.Visibility = "public"
	}
	if c.MaxRepos == 0 {
		c.MaxRepos = 100
	}
	if c.MaxCommits == 0 {
		c.MaxCommits = 1000
	}
	if c.MaxHistoryBytes == 0 {
		c.MaxHistoryBytes = 100 << 20
	}
	if c.HistoryTimeout == 0 {
		c.HistoryTimeout = 5 * time.Minute
	}
	if c.Concurrency == 0 {
		c.Concurrency = 4
	}
	if c.Mode != "fast" && c.Mode != "balanced" && c.Mode != "deep" {
		return fmt.Errorf("mode must be fast, balanced, or deep")
	}
	if c.Visibility != "public" && c.Visibility != "accessible" {
		return fmt.Errorf("visibility must be public or accessible")
	}
	if c.MaxRepos < 1 || c.MaxCommits < 1 || c.Concurrency < 1 || c.MaxHistoryBytes < 1 || c.HistoryTimeout <= 0 {
		return fmt.Errorf("repository, commit, history-byte, timeout, and concurrency limits must be positive")
	}
	if len(c.Sources) == 0 {
		c.Sources = []string{"repositories", "code", "issues", "commits"}
		if c.Mode == "balanced" || c.Mode == "deep" {
			c.Sources = append(c.Sources, "repo-history", "identity")
		}
		if c.Mode == "deep" {
			c.Sources = append(c.Sources, "git-history")
		}
	}
	seen := make(map[string]struct{})
	cleaned := c.Sources[:0]
	for _, source := range c.Sources {
		source = strings.TrimSpace(strings.ToLower(source))
		if _, ok := validSources[source]; !ok {
			return fmt.Errorf("unknown source %q", source)
		}
		if _, duplicate := seen[source]; !duplicate {
			seen[source] = struct{}{}
			cleaned = append(cleaned, source)
		}
	}
	c.Sources = cleaned
	for i, repo := range c.Repos {
		c.Repos[i] = strings.Trim(strings.TrimSpace(repo), "/")
	}
	for i, org := range c.Orgs {
		c.Orgs[i] = strings.TrimSpace(org)
	}
	return nil
}

func (c Config) HasSource(name string) bool {
	for _, source := range c.Sources {
		if source == name {
			return true
		}
	}
	return false
}

func SortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

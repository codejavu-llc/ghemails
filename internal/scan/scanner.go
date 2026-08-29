package scan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codejavu-llc/ghemails/internal/extract"
	"github.com/codejavu-llc/ghemails/internal/githubapi"
	"github.com/codejavu-llc/ghemails/internal/model"
)

type outcome struct {
	partial    string
	candidates int
}

type Scanner struct {
	config       Config
	client       *githubapi.Client
	matcher      *extract.Matcher
	aggregator   *model.Aggregator
	candidates   map[string]githubapi.Repository
	users        map[string]githubapi.User
	progress     func(string)
	candidateCap bool
}

func New(config Config, client *githubapi.Client, progress func(string)) (*Scanner, error) {
	if err := config.Normalize(); err != nil {
		return nil, err
	}
	matcher := extract.NewMatcher(config.Domain, config.IncludeSubdomains, config.IncludeNoreply)
	return &Scanner{
		config:     config,
		client:     client,
		matcher:    matcher,
		aggregator: model.NewAggregator(config.Domain, extract.Classification),
		candidates: make(map[string]githubapi.Repository),
		users:      make(map[string]githubapi.User),
		progress:   progress,
	}, nil
}

func (s *Scanner) Run(ctx context.Context) model.Run {
	run := model.Run{
		SchemaVersion: model.SchemaVersion,
		Target:        s.config.Domain,
		Mode:          s.config.Mode,
		Visibility:    s.config.Visibility,
		StartedAt:     time.Now().UTC(),
	}
	if len(s.config.Repos) > 0 || len(s.config.Orgs) > 0 {
		run.Sources = append(run.Sources, s.execute(ctx, "scope", s.scanExplicitScope))
	}
	sourceFns := []struct {
		name string
		fn   func(context.Context) (outcome, error)
	}{
		{"repositories", s.scanRepositories},
		{"code", s.scanCode},
		{"issues", s.scanIssues},
		{"commits", s.scanCommits},
		{"repo-history", s.scanRepositoryHistory},
		{"identity", s.scanIdentity},
		{"git-history", s.scanGitHistory},
	}
	for _, source := range sourceFns {
		if !s.config.HasSource(source.name) {
			continue
		}
		if ctx.Err() != nil {
			run.Sources = append(run.Sources, model.SourceStatus{Name: source.name, Status: "partial", Reason: "cancelled"})
			continue
		}
		run.Sources = append(run.Sources, s.execute(ctx, source.name, source.fn))
	}
	run.Findings = s.aggregator.Findings()
	run.CompletedAt = time.Now().UTC()
	run.Status = overallStatus(run.Sources)
	return run
}

func (s *Scanner) execute(ctx context.Context, name string, fn func(context.Context) (outcome, error)) model.SourceStatus {
	if s.progress != nil {
		s.progress(fmt.Sprintf("starting %s source", name))
	}
	started := time.Now()
	beforeFindings := s.aggregator.Len()
	before := s.client.Snapshot()
	result, err := fn(ctx)
	after := s.client.Snapshot()
	status := model.SourceStatus{
		Name:       name,
		Status:     "complete",
		Findings:   s.aggregator.Len() - beforeFindings,
		Candidates: result.candidates,
		Requests:   after.Requests - before.Requests,
		CacheHits:  after.CacheHits - before.CacheHits,
		Duration:   time.Since(started),
	}
	if result.partial != "" {
		status.Status = "partial"
		status.Reason = result.partial
	}
	if err != nil {
		if ctx.Err() != nil || status.Findings > 0 {
			status.Status = "partial"
		} else {
			status.Status = "failed"
		}
		status.Reason = err.Error()
	}
	if remaining, resetAt, ok := s.client.Rate(sourceResource(name)); ok {
		status.RateRemain = model.IntValue(remaining)
		status.RateResetAt = model.TimeValue(resetAt)
	}
	if s.progress != nil {
		s.progress(fmt.Sprintf("finished %s: %s, %d new email(s), %d request(s)", name, status.Status, status.Findings, status.Requests))
	}
	return status
}

func sourceResource(name string) string {
	if name == "code" {
		return "code_search"
	}
	if name == "repositories" || name == "issues" || name == "commits" {
		return "search"
	}
	return "core"
}

func (s *Scanner) addCandidate(repo githubapi.Repository) bool {
	fullName := strings.ToLower(strings.TrimSpace(repo.FullName))
	if fullName == "" {
		return false
	}
	if _, exists := s.candidates[fullName]; exists {
		return false
	}
	if len(s.candidates) >= s.config.MaxRepos {
		s.candidateCap = true
		return false
	}
	repo.FullName = fullName
	s.candidates[fullName] = repo
	return true
}

func (s *Scanner) addUser(user *githubapi.User) {
	if user == nil || user.Login == "" {
		return
	}
	s.users[strings.ToLower(user.Login)] = *user
}

func (s *Scanner) addText(text string, evidence model.Evidence) int {
	count := 0
	for _, email := range s.matcher.Find(text) {
		if s.aggregator.Add(email, evidence) {
			count++
		}
	}
	return count
}

func overallStatus(sources []model.SourceStatus) string {
	completed, partial, failed := 0, 0, 0
	for _, source := range sources {
		switch source.Status {
		case "complete":
			completed++
		case "partial":
			partial++
		case "failed":
			failed++
		}
	}
	if partial > 0 || (failed > 0 && completed > 0) {
		return "partial"
	}
	if failed > 0 && completed == 0 {
		return "failed"
	}
	return "complete"
}

package model

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SchemaVersion = "1"

type Evidence struct {
	Source     string     `json:"source"`
	URL        string     `json:"url,omitempty"`
	Repository string     `json:"repository,omitempty"`
	Ref        string     `json:"ref,omitempty"`
	Path       string     `json:"path,omitempty"`
	Line       int        `json:"line,omitempty"`
	CommitSHA  string     `json:"commit_sha,omitempty"`
	Actor      string     `json:"actor,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

func (e Evidence) Key() string {
	return strings.Join([]string{e.Source, e.URL, e.Repository, e.Ref, e.Path, strconv.Itoa(e.Line), e.CommitSHA, e.Actor}, "\x00")
}

type Finding struct {
	Email          string     `json:"email"`
	Domain         string     `json:"domain"`
	Classification string     `json:"classification"`
	SyntaxValid    bool       `json:"syntax_valid"`
	SourceCount    int        `json:"source_count"`
	EvidenceCount  int        `json:"evidence_count"`
	FirstSeen      *time.Time `json:"first_seen,omitempty"`
	LastSeen       *time.Time `json:"last_seen,omitempty"`
	Evidence       []Evidence `json:"evidence"`
}

type SourceStatus struct {
	Name        string        `json:"name"`
	Status      string        `json:"status"`
	Reason      string        `json:"reason,omitempty"`
	Findings    int           `json:"findings"`
	Candidates  int           `json:"candidates,omitempty"`
	Requests    int           `json:"requests,omitempty"`
	CacheHits   int           `json:"cache_hits,omitempty"`
	Duration    time.Duration `json:"duration_ns"`
	RateRemain  *int          `json:"rate_remaining,omitempty"`
	RateResetAt *time.Time    `json:"rate_reset_at,omitempty"`
}

type Run struct {
	SchemaVersion string         `json:"schema_version"`
	Target        string         `json:"target"`
	Mode          string         `json:"mode"`
	Visibility    string         `json:"visibility"`
	StartedAt     time.Time      `json:"started_at"`
	CompletedAt   time.Time      `json:"completed_at"`
	Status        string         `json:"status"`
	Findings      []Finding      `json:"findings"`
	Sources       []SourceStatus `json:"sources"`
}

type Aggregator struct {
	domain   string
	classify func(string) string
	mu       sync.Mutex
	items    map[string]*findingState
}

type findingState struct {
	finding Finding
	seen    map[string]struct{}
}

func NewAggregator(domain string, classify func(string) string) *Aggregator {
	return &Aggregator{domain: domain, classify: classify, items: make(map[string]*findingState)}
}

func (a *Aggregator) Add(email string, evidence Evidence) bool {
	email = strings.ToLower(email)
	a.mu.Lock()
	defer a.mu.Unlock()
	state, exists := a.items[email]
	if !exists {
		state = &findingState{
			finding: Finding{Email: email, Domain: a.domain, Classification: a.classify(email), SyntaxValid: true},
			seen:    make(map[string]struct{}),
		}
		a.items[email] = state
	}
	key := evidence.Key()
	if _, duplicate := state.seen[key]; duplicate {
		return !exists
	}
	state.seen[key] = struct{}{}
	state.finding.Evidence = append(state.finding.Evidence, evidence)
	if evidence.ObservedAt != nil {
		if state.finding.FirstSeen == nil || evidence.ObservedAt.Before(*state.finding.FirstSeen) {
			t := *evidence.ObservedAt
			state.finding.FirstSeen = &t
		}
		if state.finding.LastSeen == nil || evidence.ObservedAt.After(*state.finding.LastSeen) {
			t := *evidence.ObservedAt
			state.finding.LastSeen = &t
		}
	}
	return !exists
}

func TimeValue(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func IntValue(value int) *int {
	copy := value
	return &copy
}

func (a *Aggregator) Len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.items)
}

func (a *Aggregator) Findings() []Finding {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Finding, 0, len(a.items))
	for _, state := range a.items {
		finding := state.finding
		sources := make(map[string]struct{})
		sort.Slice(finding.Evidence, func(i, j int) bool { return finding.Evidence[i].Key() < finding.Evidence[j].Key() })
		for _, evidence := range finding.Evidence {
			sources[evidence.Source] = struct{}{}
		}
		finding.SourceCount = len(sources)
		finding.EvidenceCount = len(finding.Evidence)
		result = append(result, finding)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Email < result[j].Email })
	return result
}

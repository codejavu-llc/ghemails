package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codejavu-llc/ghemails/internal/githubapi"
	"github.com/codejavu-llc/ghemails/internal/model"
)

var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func (s *Scanner) visibilityQualifier() string {
	if s.config.Visibility == "public" {
		return " is:public"
	}
	return ""
}

func (s *Scanner) scanExplicitScope(ctx context.Context) (outcome, error) {
	result := outcome{}
	for _, fullName := range s.config.Repos {
		if !repoNamePattern.MatchString(fullName) {
			return result, fmt.Errorf("invalid repository %q; expected owner/name", fullName)
		}
		var repo githubapi.Repository
		endpoint := s.client.URL("repos/"+fullName, nil)
		if _, err := s.client.GetJSON(ctx, endpoint, "application/vnd.github+json", &repo); err != nil {
			return result, err
		}
		if s.config.Visibility == "public" && repo.Private {
			continue
		}
		if s.addCandidate(repo) {
			result.candidates++
		}
	}
	for _, org := range s.config.Orgs {
		if org == "" || strings.ContainsAny(org, "/ ") {
			return result, fmt.Errorf("invalid organization %q", org)
		}
		for page := 1; page <= 10 && len(s.candidates) < s.config.MaxRepos; page++ {
			values := url.Values{"type": {"all"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
			var repos []githubapi.Repository
			if _, err := s.client.GetJSON(ctx, s.client.URL("orgs/"+org+"/repos", values), "application/vnd.github+json", &repos); err != nil {
				return result, err
			}
			for _, repo := range repos {
				if s.config.Visibility == "public" && repo.Private {
					continue
				}
				if s.addCandidate(repo) {
					result.candidates++
				}
			}
			if len(repos) < 100 {
				break
			}
		}
	}
	if s.candidateCap {
		result.partial = "max-repositories"
	}
	return result, nil
}

func (s *Scanner) scanRepositories(ctx context.Context) (outcome, error) {
	query := fmt.Sprintf("\"%s\" in:readme,description%s", s.config.Domain, s.visibilityQualifier())
	seen := make(map[string]struct{})
	response, err := s.fetchRepositoryPage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	var result outcome
	if response.TotalCount > 1000 {
		result, err = s.scanRepositoryDateRange(ctx, query, time.Date(2008, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().UTC().AddDate(0, 0, 1), seen)
	} else {
		result = s.consumeRepositoryResponse(ctx, response, seen)
		for page := 2; page <= 10 && len(response.Items) == 100; page++ {
			response, err = s.fetchRepositoryPage(ctx, query, page)
			if err != nil {
				return result, err
			}
			mergeOutcome(&result, s.consumeRepositoryResponse(ctx, response, seen))
		}
	}
	if s.candidateCap {
		result.partial = joinReason(result.partial, "max-repositories")
	}
	return result, err
}

func (s *Scanner) fetchRepositoryPage(ctx context.Context, query string, page int) (githubapi.RepositorySearchResponse, error) {
	values := url.Values{"q": {query}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
	var response githubapi.RepositorySearchResponse
	_, err := s.client.GetJSON(ctx, s.client.URL("search/repositories", values), "application/vnd.github+json", &response)
	return response, err
}

func (s *Scanner) consumeRepositoryResponse(ctx context.Context, response githubapi.RepositorySearchResponse, seen map[string]struct{}) outcome {
	result := outcome{}
	if response.IncompleteResults {
		result.partial = joinReason(result.partial, "incomplete-results")
	}
	for _, repo := range response.Items {
		key := strings.ToLower(repo.FullName)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if s.addCandidate(repo) {
			result.candidates++
		}
		evidence := model.Evidence{Source: "github_repository", URL: repo.HTMLURL, Repository: repo.FullName, ObservedAt: model.TimeValue(repo.PushedAt)}
		s.addText(repo.Description, evidence)
		readmeURL := s.client.URL("repos/"+repo.FullName+"/readme", nil)
		body, _, err := s.client.Get(ctx, readmeURL, "application/vnd.github.raw+json", 2<<20)
		if err != nil {
			var apiErr *githubapi.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
				continue
			}
			result.partial = joinReason(result.partial, "readme-fetch-failed")
			continue
		}
		s.addText(string(body), evidence)
	}
	return result
}

func (s *Scanner) scanRepositoryDateRange(ctx context.Context, baseQuery string, start, end time.Time, seen map[string]struct{}) (outcome, error) {
	query := fmt.Sprintf("%s created:%s..%s", baseQuery, start.Format("2006-01-02"), end.Format("2006-01-02"))
	response, err := s.fetchRepositoryPage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	if response.TotalCount > 1000 && end.Sub(start) >= 24*time.Hour {
		days := int(end.Sub(start).Hours() / 24)
		middle := start.AddDate(0, 0, days/2)
		left, err := s.scanRepositoryDateRange(ctx, baseQuery, start, middle, seen)
		if err != nil {
			return left, err
		}
		right, err := s.scanRepositoryDateRange(ctx, baseQuery, middle.AddDate(0, 0, 1), end, seen)
		mergeOutcome(&left, right)
		return left, err
	}
	result := s.consumeRepositoryResponse(ctx, response, seen)
	if response.TotalCount > 1000 {
		result.partial = joinReason(result.partial, "repository-1000-result-limit-"+start.Format("2006-01-02"))
	}
	for page := 2; page <= 10 && len(response.Items) == 100; page++ {
		response, err = s.fetchRepositoryPage(ctx, query, page)
		if err != nil {
			return result, err
		}
		mergeOutcome(&result, s.consumeRepositoryResponse(ctx, response, seen))
	}
	return result, nil
}

func (s *Scanner) scanCode(ctx context.Context) (outcome, error) {
	query := fmt.Sprintf("\"@%s\" in:file%s", s.config.Domain, s.visibilityQualifier())
	seen := make(map[string]struct{})
	response, err := s.fetchCodePage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	if response.TotalCount > 1000 {
		result, err := s.scanCodeSizeRange(ctx, query, 0, 384, seen)
		if s.candidateCap {
			result.partial = joinReason(result.partial, "max-repositories")
		}
		return result, err
	}
	result := s.consumeCodeResponse(ctx, response, seen)
	for page := 2; page <= 10 && len(response.Items) == 100; page++ {
		response, err = s.fetchCodePage(ctx, query, page)
		if err != nil {
			return result, err
		}
		mergeOutcome(&result, s.consumeCodeResponse(ctx, response, seen))
	}
	if s.candidateCap {
		result.partial = joinReason(result.partial, "max-repositories")
	}
	return result, nil
}

func (s *Scanner) fetchCodePage(ctx context.Context, query string, page int) (githubapi.CodeSearchResponse, error) {
	values := url.Values{"q": {query}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
	var response githubapi.CodeSearchResponse
	_, err := s.client.GetJSON(ctx, s.client.URL("search/code", values), "application/vnd.github+json", &response)
	return response, err
}

func (s *Scanner) consumeCodeResponse(ctx context.Context, response githubapi.CodeSearchResponse, seen map[string]struct{}) outcome {
	result := outcome{}
	if response.IncompleteResults {
		result.partial = joinReason(result.partial, "incomplete-results")
	}
	for _, item := range response.Items {
		key := strings.ToLower(item.Repository.FullName) + "\x00" + item.Path + "\x00" + item.SHA
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if s.addCandidate(item.Repository) {
			result.candidates++
		}
	}
	if s.scanCodeItems(ctx, response.Items) > 0 {
		result.partial = joinReason(result.partial, "content-fetch-failed")
	}
	return result
}

func (s *Scanner) scanCodeSizeRange(ctx context.Context, baseQuery string, minimum, maximum int, seen map[string]struct{}) (outcome, error) {
	query := fmt.Sprintf("%s size:%d..%d", baseQuery, minimum, maximum)
	response, err := s.fetchCodePage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	if response.TotalCount > 1000 && minimum < maximum {
		middle := minimum + (maximum-minimum)/2
		left, err := s.scanCodeSizeRange(ctx, baseQuery, minimum, middle, seen)
		if err != nil {
			return left, err
		}
		right, err := s.scanCodeSizeRange(ctx, baseQuery, middle+1, maximum, seen)
		mergeOutcome(&left, right)
		return left, err
	}
	result := s.consumeCodeResponse(ctx, response, seen)
	if response.TotalCount > 1000 {
		result.partial = joinReason(result.partial, fmt.Sprintf("github-1000-result-limit-size-%d", minimum))
	}
	for page := 2; page <= 10 && len(response.Items) == 100; page++ {
		response, err = s.fetchCodePage(ctx, query, page)
		if err != nil {
			return result, err
		}
		mergeOutcome(&result, s.consumeCodeResponse(ctx, response, seen))
	}
	return result, nil
}

func mergeOutcome(target *outcome, next outcome) {
	target.partial = joinReason(target.partial, next.partial)
	target.candidates += next.candidates
}

func (s *Scanner) scanCodeItems(ctx context.Context, items []githubapi.CodeItem) int {
	workers := s.config.Concurrency
	if workers > len(items) {
		workers = len(items)
	}
	if workers == 0 {
		return 0
	}
	jobs := make(chan githubapi.CodeItem)
	failed := make(chan struct{}, len(items))
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for item := range jobs {
				contentURL, accept := item.URL, "application/vnd.github.raw+json"
				if raw, ok := rawURL(item.HTMLURL); ok {
					contentURL, accept = raw, "application/octet-stream"
				}
				body, _, err := s.client.Get(ctx, contentURL, accept, 2<<20)
				if err != nil {
					failed <- struct{}{}
					continue
				}
				for lineNumber, line := range strings.Split(string(body), "\n") {
					evidence := model.Evidence{
						Source: "github_code", URL: item.HTMLURL + "#L" + strconv.Itoa(lineNumber+1), Repository: item.Repository.FullName,
						Ref: item.SHA, Path: item.Path, Line: lineNumber + 1,
					}
					s.addText(line, evidence)
				}
			}
		}()
	}
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	group.Wait()
	return len(failed)
}

func rawURL(htmlURL string) (string, bool) {
	parsed, err := url.Parse(htmlURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", false
	}
	marker := "/blob/"
	if !strings.Contains(parsed.Path, marker) {
		return "", false
	}
	if strings.EqualFold(parsed.Hostname(), "github.com") {
		parsed.Host = "raw.githubusercontent.com"
		parsed.Path = strings.Replace(parsed.Path, marker, "/", 1)
	} else {
		parsed.Path = strings.Replace(parsed.Path, marker, "/raw/", 1)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

func (s *Scanner) scanIssues(ctx context.Context) (outcome, error) {
	result := outcome{}
	for _, kind := range []string{"issue", "pr"} {
		query := fmt.Sprintf("\"@%s\" in:title,body,comments is:%s%s", s.config.Domain, kind, s.visibilityQualifier())
		response, err := s.fetchIssuePage(ctx, query, 1)
		if err != nil {
			return result, err
		}
		var current outcome
		if response.TotalCount > 1000 {
			current, err = s.scanIssueDateRange(ctx, query, kind, time.Date(2008, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().UTC().AddDate(0, 0, 1))
		} else {
			current = s.consumeIssueResponse(response, kind)
			for page := 2; page <= 10 && len(response.Items) == 100; page++ {
				response, err = s.fetchIssuePage(ctx, query, page)
				if err != nil {
					break
				}
				mergeOutcome(&current, s.consumeIssueResponse(response, kind))
			}
		}
		mergeOutcome(&result, current)
		if err != nil {
			return result, err
		}
	}
	if s.candidateCap {
		result.partial = joinReason(result.partial, "max-repositories")
	}
	return result, nil
}

func (s *Scanner) fetchIssuePage(ctx context.Context, query string, page int) (githubapi.IssueSearchResponse, error) {
	values := url.Values{"q": {query}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
	var response githubapi.IssueSearchResponse
	_, err := s.client.GetJSON(ctx, s.client.URL("search/issues", values), "application/vnd.github.text-match+json", &response)
	return response, err
}

func (s *Scanner) consumeIssueResponse(response githubapi.IssueSearchResponse, kind string) outcome {
	result := outcome{}
	if response.IncompleteResults {
		result.partial = "incomplete-results"
	}
	for _, item := range response.Items {
		repoName := repositoryNameFromAPIURL(item.RepositoryURL)
		if s.addCandidate(githubapi.Repository{FullName: repoName, HTMLURL: "https://github.com/" + repoName}) {
			result.candidates++
		}
		s.addUser(&item.User)
		evidence := model.Evidence{Source: "github_" + kind, URL: item.HTMLURL, Repository: repoName, Actor: item.User.Login, ObservedAt: model.TimeValue(item.CreatedAt)}
		s.addText(item.Title+"\n"+item.Body, evidence)
		for _, match := range item.TextMatches {
			matchEvidence := evidence
			if match.ObjectURL != "" {
				matchEvidence.URL = match.ObjectURL
			}
			if match.Property == "body" && strings.Contains(match.ObjectURL, "/comments/") {
				matchEvidence.Source = "github_" + kind + "_comment"
				matchEvidence.Actor = ""
			}
			s.addText(match.Fragment, matchEvidence)
		}
	}
	return result
}

func (s *Scanner) scanIssueDateRange(ctx context.Context, baseQuery, kind string, start, end time.Time) (outcome, error) {
	query := fmt.Sprintf("%s created:%s..%s", baseQuery, start.Format("2006-01-02"), end.Format("2006-01-02"))
	response, err := s.fetchIssuePage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	if response.TotalCount > 1000 && end.Sub(start) >= 24*time.Hour {
		days := int(end.Sub(start).Hours() / 24)
		middle := start.AddDate(0, 0, days/2)
		left, err := s.scanIssueDateRange(ctx, baseQuery, kind, start, middle)
		if err != nil {
			return left, err
		}
		right, err := s.scanIssueDateRange(ctx, baseQuery, kind, middle.AddDate(0, 0, 1), end)
		mergeOutcome(&left, right)
		return left, err
	}
	result := s.consumeIssueResponse(response, kind)
	if response.TotalCount > 1000 {
		result.partial = joinReason(result.partial, kind+"-1000-result-limit-"+start.Format("2006-01-02"))
	}
	for page := 2; page <= 10 && len(response.Items) == 100; page++ {
		response, err = s.fetchIssuePage(ctx, query, page)
		if err != nil {
			return result, err
		}
		mergeOutcome(&result, s.consumeIssueResponse(response, kind))
	}
	return result, nil
}

func (s *Scanner) scanCommits(ctx context.Context) (outcome, error) {
	query := fmt.Sprintf("\"@%s\"%s", s.config.Domain, s.visibilityQualifier())
	response, err := s.fetchCommitPage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	var result outcome
	if response.TotalCount > 1000 {
		result, err = s.scanCommitDateRange(ctx, query, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().UTC().AddDate(0, 0, 1))
	} else {
		result = s.consumeCommitResponse(response)
		for page := 2; page <= 10 && len(response.Items) == 100; page++ {
			response, err = s.fetchCommitPage(ctx, query, page)
			if err != nil {
				break
			}
			mergeOutcome(&result, s.consumeCommitResponse(response))
		}
	}
	if s.candidateCap {
		result.partial = joinReason(result.partial, "max-repositories")
	}
	return result, err
}

func (s *Scanner) fetchCommitPage(ctx context.Context, query string, page int) (githubapi.CommitSearchResponse, error) {
	values := url.Values{"q": {query}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
	var response githubapi.CommitSearchResponse
	_, err := s.client.GetJSON(ctx, s.client.URL("search/commits", values), "application/vnd.github+json", &response)
	return response, err
}

func (s *Scanner) consumeCommitResponse(response githubapi.CommitSearchResponse) outcome {
	result := outcome{}
	if response.IncompleteResults {
		result.partial = "incomplete-results"
	}
	for _, item := range response.Items {
		if s.addCandidate(item.Repository) {
			result.candidates++
		}
		s.addUser(item.Author)
		s.addUser(item.Committer)
		s.addCommitItem(item, "github_commit_search")
	}
	return result
}

func (s *Scanner) scanCommitDateRange(ctx context.Context, baseQuery string, start, end time.Time) (outcome, error) {
	query := fmt.Sprintf("%s committer-date:%s..%s", baseQuery, start.Format("2006-01-02"), end.Format("2006-01-02"))
	response, err := s.fetchCommitPage(ctx, query, 1)
	if err != nil {
		return outcome{}, err
	}
	if response.TotalCount > 1000 && end.Sub(start) >= 24*time.Hour {
		days := int(end.Sub(start).Hours() / 24)
		middle := start.AddDate(0, 0, days/2)
		left, err := s.scanCommitDateRange(ctx, baseQuery, start, middle)
		if err != nil {
			return left, err
		}
		right, err := s.scanCommitDateRange(ctx, baseQuery, middle.AddDate(0, 0, 1), end)
		mergeOutcome(&left, right)
		return left, err
	}
	result := s.consumeCommitResponse(response)
	if response.TotalCount > 1000 {
		result.partial = joinReason(result.partial, "commit-1000-result-limit-"+start.Format("2006-01-02"))
	}
	for page := 2; page <= 10 && len(response.Items) == 100; page++ {
		response, err = s.fetchCommitPage(ctx, query, page)
		if err != nil {
			return result, err
		}
		mergeOutcome(&result, s.consumeCommitResponse(response))
	}
	return result, nil
}

func (s *Scanner) addCommitItem(item githubapi.CommitItem, source string) {
	repoName := item.Repository.FullName
	base := model.Evidence{Source: source + "_message", URL: item.HTMLURL, Repository: repoName, CommitSHA: item.SHA, ObservedAt: model.TimeValue(item.Commit.Author.Date)}
	s.addText(item.Commit.Message, base)
	authorEvidence := base
	authorEvidence.Source = source + "_author"
	authorEvidence.Actor = item.Commit.Author.Name
	s.addText(item.Commit.Author.Email, authorEvidence)
	committerEvidence := base
	committerEvidence.Source = source + "_committer"
	committerEvidence.Actor = item.Commit.Committer.Name
	committerEvidence.ObservedAt = model.TimeValue(item.Commit.Committer.Date)
	s.addText(item.Commit.Committer.Email, committerEvidence)
}

func (s *Scanner) scanRepositoryHistory(ctx context.Context) (outcome, error) {
	result := outcome{}
	for _, fullName := range SortedKeys(s.candidates) {
		repo := s.candidates[fullName]
		count := 0
		for page := 1; count < s.config.MaxCommits; page++ {
			perPage := 100
			if remaining := s.config.MaxCommits - count; remaining < perPage {
				perPage = remaining
			}
			values := url.Values{"per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}
			var commits []githubapi.CommitItem
			if _, err := s.client.GetJSON(ctx, s.client.URL("repos/"+fullName+"/commits", values), "application/vnd.github+json", &commits); err != nil {
				result.partial = joinReason(result.partial, "repository-commit-fetch-failed")
				break
			}
			for _, item := range commits {
				item.Repository = repo
				if item.HTMLURL == "" {
					item.HTMLURL = strings.TrimRight(repo.HTMLURL, "/") + "/commit/" + item.SHA
				}
				s.addUser(item.Author)
				s.addUser(item.Committer)
				s.addCommitItem(item, "github_repo_commit")
			}
			count += len(commits)
			if len(commits) < perPage {
				break
			}
			if count >= s.config.MaxCommits {
				result.partial = joinReason(result.partial, "max-commits-per-repository")
			}
		}
	}
	result.candidates = len(s.candidates)
	return result, nil
}

func (s *Scanner) scanIdentity(ctx context.Context) (outcome, error) {
	result := outcome{candidates: len(s.users)}
	for _, login := range SortedKeys(s.users) {
		var user githubapi.User
		if _, err := s.client.GetJSON(ctx, s.client.URL("users/"+login, nil), "application/vnd.github+json", &user); err != nil {
			result.partial = joinReason(result.partial, "profile-fetch-failed")
			continue
		}
		profileEvidence := model.Evidence{Source: "github_profile", URL: user.HTMLURL, Actor: user.Login}
		s.addText(strings.Join([]string{user.Email, user.Bio, user.Company, user.Blog}, "\n"), profileEvidence)
		for page := 1; page <= 3; page++ {
			values := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}
			var events []githubapi.Event
			if _, err := s.client.GetJSON(ctx, s.client.URL("users/"+login+"/events/public", values), "application/vnd.github+json", &events); err != nil {
				result.partial = joinReason(result.partial, "event-fetch-failed")
				break
			}
			for _, event := range events {
				payload, _ := json.Marshal(event.Payload)
				evidence := model.Evidence{Source: "github_public_event", URL: user.HTMLURL + "?tab=activity", Repository: event.Repo.Name, Actor: login, ObservedAt: model.TimeValue(event.CreatedAt)}
				s.addText(string(payload), evidence)
			}
			if len(events) < 100 {
				break
			}
		}
	}
	return result, nil
}

func repositoryNameFromAPIURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "repos" {
			return parts[i+1] + "/" + parts[i+2]
		}
	}
	return ""
}

func joinReason(current, next string) string {
	if current == "" {
		return next
	}
	for _, existing := range strings.Split(current, ",") {
		if existing == next {
			return current
		}
	}
	parts := append(strings.Split(current, ","), next)
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func parseGitDate(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

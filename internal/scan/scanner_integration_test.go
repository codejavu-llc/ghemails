package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codejavu-llc/ghemails/internal/cache"
	"github.com/codejavu-llc/ghemails/internal/githubapi"
)

func TestBalancedScannerEvidencePlanes(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/search/repositories":
			writeTestJSON(writer, map[string]any{"total_count": 1, "items": []any{testRepository("desc@example.com")}})
		case "/repos/acme/repo/readme":
			_, _ = fmt.Fprint(writer, "readme@example.com")
		case "/search/code":
			writeTestJSON(writer, map[string]any{"total_count": 1, "items": []any{map[string]any{
				"path": "contact.txt", "sha": "code-sha", "url": server.URL + "/content", "html_url": server.URL + "/acme/repo/blob/code-sha/contact.txt",
				"repository": testRepository(""),
			}}})
		case "/content":
			_, _ = fmt.Fprint(writer, "code@example.com")
		case "/search/issues":
			kindEmail := "issue@example.com"
			if strings.Contains(request.URL.Query().Get("q"), "is:pr") {
				kindEmail = "pr@example.com"
			}
			writeTestJSON(writer, map[string]any{"total_count": 1, "items": []any{map[string]any{
				"number": 1, "html_url": server.URL + "/acme/repo/issues/1", "body": kindEmail,
				"repository_url": server.URL + "/repos/acme/repo", "created_at": "2025-01-01T00:00:00Z",
				"user":         map[string]any{"login": "alice", "html_url": server.URL + "/alice"},
				"text_matches": []any{map[string]any{"property": "body", "object_url": server.URL + "/repos/acme/repo/issues/comments/1", "fragment": "comment@example.com"}},
			}}})
		case "/search/commits":
			writeTestJSON(writer, map[string]any{"total_count": 1, "items": []any{testCommit(server.URL, "search-sha", "message@example.com", "author@example.com", "committer@example.com")}})
		case "/repos/acme/repo/commits":
			writeTestJSON(writer, []any{testCommit(server.URL, "history-sha", "", "history@example.com", "other@other.test")})
		case "/users/alice":
			writeTestJSON(writer, map[string]any{"login": "alice", "html_url": server.URL + "/alice", "email": "profile@example.com"})
		case "/users/alice/events/public":
			writeTestJSON(writer, []any{map[string]any{"id": "1", "created_at": "2025-01-02T00:00:00Z", "repo": map[string]any{"name": "acme/repo"}, "payload": map[string]any{"email": "event@example.com"}}})
		case "/users/bob":
			writeTestJSON(writer, map[string]any{"login": "bob", "html_url": server.URL + "/bob"})
		case "/users/bob/events/public":
			writeTestJSON(writer, []any{})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := githubapi.NewClient(server.URL, "token", time.Second, &cache.Cache{Enabled: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{Domain: "example.com", Mode: "balanced", MaxRepos: 10, MaxCommits: 10}, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	run := scanner.Run(context.Background())
	if run.Status != "complete" {
		t.Fatalf("run status=%s sources=%#v", run.Status, run.Sources)
	}
	found := make(map[string]bool)
	for _, finding := range run.Findings {
		found[finding.Email] = true
	}
	for _, email := range []string{
		"desc@example.com", "readme@example.com", "code@example.com", "issue@example.com", "pr@example.com", "comment@example.com",
		"message@example.com", "author@example.com", "committer@example.com", "history@example.com", "profile@example.com", "event@example.com",
	} {
		if !found[email] {
			t.Errorf("missing %s in %#v", email, found)
		}
	}
}

func testRepository(description string) map[string]any {
	return map[string]any{
		"full_name": "acme/repo", "html_url": "https://github.com/acme/repo", "clone_url": "https://github.com/acme/repo.git",
		"description": description, "default_branch": "main", "visibility": "public", "pushed_at": "2025-01-01T00:00:00Z",
	}
}

func testCommit(baseURL, sha, message, authorEmail, committerEmail string) map[string]any {
	return map[string]any{
		"sha": sha, "html_url": baseURL + "/acme/repo/commit/" + sha, "repository": testRepository(""),
		"author":    map[string]any{"login": "alice", "html_url": baseURL + "/alice"},
		"committer": map[string]any{"login": "bob", "html_url": baseURL + "/bob"},
		"commit": map[string]any{
			"message":   message,
			"author":    map[string]any{"name": "Alice", "email": authorEmail, "date": "2025-01-01T00:00:00Z"},
			"committer": map[string]any{"name": "Bob", "email": committerEmail, "date": "2025-01-01T00:00:01Z"},
		},
	}
}

func writeTestJSON(writer http.ResponseWriter, value any) {
	_ = json.NewEncoder(writer).Encode(value)
}

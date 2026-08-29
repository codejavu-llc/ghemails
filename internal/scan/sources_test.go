package scan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codejavu-llc/ghemails/internal/cache"
	"github.com/codejavu-llc/ghemails/internal/githubapi"
)

func TestRepositorySearchPartitionsOversizedResults(t *testing.T) {
	t.Parallel()
	var searchRequests atomic.Int32
	var leafResponses atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/search/repositories" {
			if strings.HasSuffix(request.URL.Path, "/readme") {
				_, _ = fmt.Fprint(writer, "readme content")
				return
			}
			http.NotFound(writer, request)
			return
		}
		searchRequests.Add(1)
		query := request.URL.Query().Get("q")
		dateRange := ""
		for _, field := range strings.Fields(query) {
			if strings.HasPrefix(field, "created:") {
				dateRange = strings.TrimPrefix(field, "created:")
				break
			}
		}
		if dateRange == "" {
			writeTestJSON(writer, map[string]any{"total_count": 1001, "items": []any{}})
			return
		}
		bounds := strings.SplitN(dateRange, "..", 2)
		start, startErr := time.Parse("2006-01-02", bounds[0])
		end, endErr := time.Parse("2006-01-02", bounds[1])
		if startErr != nil || endErr != nil {
			http.Error(writer, "invalid date range", http.StatusBadRequest)
			return
		}
		if end.Sub(start) > 5000*24*time.Hour {
			writeTestJSON(writer, map[string]any{"total_count": 1001, "items": []any{}})
			return
		}
		leaf := leafResponses.Add(1)
		repository := testRepository(fmt.Sprintf("leaf%d@example.com", leaf))
		repository["full_name"] = fmt.Sprintf("acme/repo%d", leaf)
		writeTestJSON(writer, map[string]any{"total_count": 1, "items": []any{repository}})
	}))
	defer server.Close()

	client, err := githubapi.NewClient(server.URL, "token", time.Second, &cache.Cache{Enabled: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := New(Config{Domain: "example.com", Mode: "fast", MaxRepos: 10}, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.scanRepositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.partial != "" || result.candidates != 2 {
		t.Fatalf("scanRepositories() = %#v", result)
	}
	if got := searchRequests.Load(); got != 4 {
		t.Fatalf("search requests = %d, want 4", got)
	}
	if got := scanner.aggregator.Len(); got != 2 {
		t.Fatalf("findings = %d, want 2", got)
	}
}

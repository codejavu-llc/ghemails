package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteCodeSearchEndToEnd(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/search/code":
			response := map[string]any{
				"total_count": 1, "incomplete_results": false,
				"items": []any{map[string]any{
					"path": "README.md", "sha": "abc", "url": server.URL + "/content",
					"html_url":   server.URL + "/acme/repo/blob/abc/README.md",
					"repository": map[string]any{"full_name": "acme/repo", "html_url": "https://github.com/acme/repo"},
				}},
			}
			_ = json.NewEncoder(writer).Encode(response)
		case "/content":
			_, _ = fmt.Fprintln(writer, "Contact Security@Example.com; ignore x@example.com.evil")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "-d", "example.com", "--sources", "code", "--api-url", server.URL, "--token-file", tokenFile, "--no-cache"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Execute() code=%d stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "security@example.com" {
		t.Fatalf("stdout=%q", got)
	}
	if !strings.Contains(stderr.String(), "finished code: complete") {
		t.Fatalf("missing source status: %s", stderr.String())
	}
}

func TestExecuteRequiresDomainAndToken(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan"}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing domain exit=%d; stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if code := Execute([]string{"scan", "-d", "example.com"}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing token exit=%d; stderr=%s", code, stderr.String())
	}
}

func TestOutputWriterRejectsSymlink(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("symlink behavior differs on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "report")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := outputWriter(options{output: link, force: true}, &bytes.Buffer{}); err == nil {
		t.Fatal("outputWriter accepted a symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("target changed: %q, %v", data, err)
	}
}

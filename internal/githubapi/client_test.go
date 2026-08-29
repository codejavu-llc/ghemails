package githubapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codejavu-llc/ghemails/internal/cache"
)

func TestClientAuthenticationCachingAndLimits(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		writer.Header().Set("X-RateLimit-Resource", "code_search")
		writer.Header().Set("X-RateLimit-Remaining", "9")
		writer.Header().Set("X-RateLimit-Reset", "2000000000")
		_, _ = io.WriteString(writer, "{\"value\":\"ok\"}")
	}))
	defer server.Close()
	cached := &cache.Cache{Dir: filepath.Join(t.TempDir(), "cache"), TTL: time.Hour, Enabled: true}
	client, err := NewClient(server.URL, "test-token", time.Second, cached, nil)
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]string
	meta, err := client.GetJSON(context.Background(), client.URL("search/code", nil), "application/json", &target)
	if err != nil {
		t.Fatal(err)
	}
	if target["value"] != "ok" || meta.Resource != "code_search" || meta.Remaining != 9 {
		t.Fatalf("unexpected result: %#v, %#v", target, meta)
	}
	target = nil
	meta, err = client.GetJSON(context.Background(), client.URL("search/code", nil), "application/json", &target)
	if err != nil || !meta.Cached || calls.Load() != 1 {
		t.Fatalf("cache result: meta=%#v calls=%d err=%v", meta, calls.Load(), err)
	}
}

func TestClientStripsTokenAcrossOrigins(t *testing.T) {
	t.Parallel()
	var leaked atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		leaked.Store(request.Header.Get("Authorization") != "")
		_, _ = io.WriteString(writer, "{}")
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL, http.StatusFound)
	}))
	defer source.Close()
	client, err := NewClient(source.URL, "do-not-leak", time.Second, &cache.Cache{Enabled: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Get(context.Background(), source.URL, "application/json", 1024); err != nil {
		t.Fatal(err)
	}
	if leaked.Load() {
		t.Fatal("authorization token crossed redirect origin")
	}
}

func TestClientRejectsUnsafeBaseAndOversizedBody(t *testing.T) {
	t.Parallel()
	if _, err := NewClient("http://example.com", "", time.Second, nil, nil); err == nil {
		t.Fatal("unsafe HTTP API URL accepted")
	}
	if _, err := NewClient("https://token@example.com", "", time.Second, nil, nil); err == nil {
		t.Fatal("credential-bearing API URL accepted")
	}
	if _, err := NewClient(":%", "", time.Second, nil, nil); err == nil {
		t.Fatal("malformed API URL accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "12345")
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", time.Second, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Get(context.Background(), server.URL, "text/plain", 4); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestCacheIsIsolatedByCredential(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(writer, request.Header.Get("Authorization"))
	}))
	defer server.Close()
	cached := &cache.Cache{Dir: filepath.Join(t.TempDir(), "cache"), TTL: time.Hour, Enabled: true}
	first, err := NewClient(server.URL, "first", time.Second, cached, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewClient(server.URL, "second", time.Second, cached, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstBody, _, err := first.Get(context.Background(), server.URL, "text/plain", 1024)
	if err != nil {
		t.Fatal(err)
	}
	secondBody, _, err := second.Get(context.Background(), server.URL, "text/plain", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBody) != "Bearer first" || string(secondBody) != "Bearer second" || calls.Load() != 2 {
		t.Fatalf("cache crossed credentials: first=%q second=%q calls=%d", firstBody, secondBody, calls.Load())
	}
}

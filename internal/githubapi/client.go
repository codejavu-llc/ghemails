package githubapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codejavu-llc/ghemails/internal/cache"
)

const defaultMaxBody = 16 << 20

type Counters struct {
	Requests  int
	CacheHits int
}

type Metadata struct {
	StatusCode int
	Resource   string
	Remaining  int
	ResetAt    time.Time
	Cached     bool
}

type APIError struct {
	StatusCode int
	URL        string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub API HTTP %d for %s: %s", e.StatusCode, e.URL, e.Message)
}

type rateState struct {
	last      time.Time
	remaining int
	resetAt   time.Time
}

type Client struct {
	base      *url.URL
	token     string
	http      *http.Client
	cache     *cache.Cache
	userAgent string
	cacheNS   string
	approved  map[string]struct{}
	mu        sync.Mutex
	rates     map[string]rateState
	counters  Counters
	notify    func(string)
	local     bool
}

func NewClient(baseURL, token string, timeout time.Duration, cached *cache.Cache, notify func(string)) (*Client, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || base == nil {
		return nil, fmt.Errorf("invalid GitHub API URL %q", baseURL)
	}
	loopbackHTTP := base.Scheme == "http" && net.ParseIP(base.Hostname()) != nil && net.ParseIP(base.Hostname()).IsLoopback()
	if (base.Scheme != "https" && !loopbackHTTP) || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid GitHub API URL %q", baseURL)
	}
	approved := map[string]struct{}{strings.ToLower(base.Host): {}}
	if base.Hostname() == "api.github.com" {
		approved["raw.githubusercontent.com"] = struct{}{}
		approved["github.com"] = struct{}{}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 8
	cacheIdentity := sha256.Sum256([]byte(strings.ToLower(base.Scheme+"://"+base.Host) + "\x00" + base.EscapedPath() + "\x00" + token))
	client := &Client{
		base:      base,
		token:     token,
		cache:     cached,
		userAgent: "ghemails/1.0",
		cacheNS:   fmt.Sprintf("%x", cacheIdentity),
		approved:  approved,
		rates:     make(map[string]rateState),
		notify:    notify,
		local:     loopbackHTTP,
	}
	client.http = &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if _, ok := client.approved[strings.ToLower(req.URL.Host)]; !ok {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	return client, nil
}

func (c *Client) URL(path string, values url.Values) string {
	endpoint := strings.TrimRight(c.base.String(), "/") + "/" + strings.TrimLeft(path, "/")
	if len(values) > 0 {
		endpoint += "?" + values.Encode()
	}
	return endpoint
}

func (c *Client) Snapshot() Counters {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counters
}

func (c *Client) Rate(resource string) (int, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.rates[resource]
	return state.remaining, state.resetAt, ok && (!state.resetAt.IsZero() || state.remaining != 0)
}

func (c *Client) GetJSON(ctx context.Context, endpoint, accept string, target any) (Metadata, error) {
	body, meta, err := c.Get(ctx, endpoint, accept, defaultMaxBody)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return meta, fmt.Errorf("decode GitHub response: %w", err)
	}
	return meta, nil
}

func (c *Client) Get(ctx context.Context, endpoint, accept string, maxBody int64) ([]byte, Metadata, error) {
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Hostname() == "" || parsed.User != nil {
		return nil, Metadata{}, fmt.Errorf("invalid request URL %q", endpoint)
	}
	if parsed.Scheme != "https" && !(c.local && parsed.Scheme == "http" && strings.EqualFold(parsed.Host, c.base.Host)) {
		return nil, Metadata{}, fmt.Errorf("refusing insecure request URL %q", endpoint)
	}
	cacheKey := c.cacheNS + "\x00" + accept + "\x00" + endpoint
	if cached, ok := c.cache.Get(cacheKey); ok {
		c.mu.Lock()
		c.counters.CacheHits++
		c.mu.Unlock()
		return cached, Metadata{StatusCode: http.StatusOK, Cached: true}, nil
	}
	resource := resourceFor(parsed.Path)
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if err := c.wait(ctx, resource); err != nil {
			return nil, Metadata{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, Metadata{}, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.token != "" {
			if _, ok := c.approved[strings.ToLower(parsed.Host)]; ok {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
		}
		resp, err := c.http.Do(req)
		c.mu.Lock()
		c.counters.Requests++
		c.mu.Unlock()
		if err != nil {
			lastErr = err
			if !sleepContext(ctx, backoff(attempt)) {
				return nil, Metadata{}, ctx.Err()
			}
			continue
		}
		body, readErr := readLimited(resp.Body, maxBody)
		resp.Body.Close()
		meta := metadata(resp, resource)
		c.recordRate(resource, meta)
		if readErr != nil {
			return nil, meta, readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = c.cache.Put(cacheKey, body)
			return body, meta, nil
		}
		message := parseMessage(body)
		lastErr = &APIError{StatusCode: resp.StatusCode, URL: endpoint, Message: message}
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return nil, meta, lastErr
		}
		wait := retryDelay(resp.Header, meta, attempt)
		if c.notify != nil {
			c.notify(fmt.Sprintf("rate/provider response HTTP %d; retrying in %s", resp.StatusCode, wait.Round(time.Second)))
		}
		if !sleepContext(ctx, wait) {
			return nil, meta, ctx.Err()
		}
	}
	return nil, Metadata{}, lastErr
}

func (c *Client) wait(ctx context.Context, resource string) error {
	c.mu.Lock()
	state := c.rates[resource]
	delay := time.Duration(0)
	if state.remaining == 0 && state.resetAt.After(time.Now()) {
		delay = time.Until(state.resetAt) + time.Second
	}
	minimum := time.Duration(0)
	if !c.local && resource == "code_search" {
		minimum = 6 * time.Second
	} else if !c.local && resource == "search" {
		minimum = 2 * time.Second
	}
	if since := time.Since(state.last); minimum > 0 && since < minimum && minimum-since > delay {
		delay = minimum - since
	}
	state.last = time.Now().Add(delay)
	c.rates[resource] = state
	c.mu.Unlock()
	if delay > 0 && c.notify != nil {
		c.notify(fmt.Sprintf("waiting %s for GitHub %s quota", delay.Round(time.Second), resource))
	}
	if !sleepContext(ctx, delay) {
		return ctx.Err()
	}
	return nil
}

func (c *Client) recordRate(resource string, meta Metadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.rates[resource]
	state.remaining = meta.Remaining
	state.resetAt = meta.ResetAt
	c.rates[resource] = state
}

func resourceFor(path string) string {
	if strings.HasPrefix(path, "/search/code") {
		return "code_search"
	}
	if strings.HasPrefix(path, "/search/") {
		return "search"
	}
	return "core"
}

func metadata(resp *http.Response, fallback string) Metadata {
	remaining, _ := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	resetUnix, _ := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	resource := resp.Header.Get("X-RateLimit-Resource")
	if resource == "" {
		resource = fallback
	}
	var reset time.Time
	if resetUnix > 0 {
		reset = time.Unix(resetUnix, 0).UTC()
	}
	return Metadata{StatusCode: resp.StatusCode, Resource: resource, Remaining: remaining, ResetAt: reset}
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte safety limit", limit)
	}
	return body, nil
}

func parseMessage(body []byte) string {
	var value struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &value) == nil && value.Message != "" {
		return value.Message
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 300 {
		message = message[:300] + "..."
	}
	return message
}

func retryDelay(header http.Header, meta Metadata, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if meta.Remaining == 0 && meta.ResetAt.After(time.Now()) {
		return time.Until(meta.ResetAt) + time.Second
	}
	return backoff(attempt)
}

func backoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * time.Second
	return base + time.Duration(rand.Intn(500))*time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

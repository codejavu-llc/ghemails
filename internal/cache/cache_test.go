package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheRoundTripAndExpiry(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "cache")
	value := &Cache{Dir: dir, TTL: time.Hour, Enabled: true}
	if err := value.Put("key", []byte("secret@example.com")); err != nil {
		t.Fatal(err)
	}
	got, ok := value.Get("key")
	if !ok || string(got) != "secret@example.com" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}
	info, err := os.Stat(value.path("key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %o; want 600", info.Mode().Perm())
	}
	value.TTL = -time.Second
	if _, ok := value.Get("key"); ok {
		t.Fatal("expired entry was returned")
	}
}

func TestClearRejectsBroadTargets(t *testing.T) {
	t.Parallel()
	if err := Clear(string(filepath.Separator)); err == nil {
		t.Fatal("Clear(root) unexpectedly succeeded")
	}
	unmarked := t.TempDir()
	keep := filepath.Join(unmarked, "keep.txt")
	if err := os.WriteFile(keep, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Clear(unmarked); err == nil {
		t.Fatal("Clear(unmarked directory) unexpectedly succeeded")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}

func TestClearRemovesOnlyCacheFiles(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "cache")
	value := &Cache{Dir: dir, TTL: time.Hour, Enabled: true}
	if err := value.Put("key", []byte("value")); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(value.path("key")); !os.IsNotExist(err) {
		t.Fatalf("cache entry still exists: %v", err)
	}
	data, err := os.ReadFile(keep)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("unrelated file changed: %q, %v", data, err)
	}
}

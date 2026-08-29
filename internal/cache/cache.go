package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	markerName    = ".ghemails-cache"
	markerContent = "ghemails cache v1\n"
)

var entryName = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

type Cache struct {
	Dir     string
	TTL     time.Duration
	Enabled bool
}

type entry struct {
	StoredAt time.Time `json:"stored_at"`
	Body     []byte    `json:"body"`
}

func DefaultDir() string {
	if base, err := os.UserCacheDir(); err == nil {
		return filepath.Join(base, "ghemails")
	}
	return filepath.Join(os.TempDir(), "ghemails-cache")
}

func (c *Cache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.Dir, hex.EncodeToString(sum[:])+".json")
}

func (c *Cache) Get(key string) ([]byte, bool) {
	if c == nil || !c.Enabled {
		return nil, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var cached entry
	if json.Unmarshal(data, &cached) != nil || time.Since(cached.StoredAt) > c.TTL {
		return nil, false
	}
	return cached.Body, true
}

func (c *Cache) Put(key string, body []byte) error {
	if c == nil || !c.Enabled {
		return nil
	}
	if err := ensureDirectory(c.Dir); err != nil {
		return err
	}
	data, err := json.Marshal(entry{StoredAt: time.Now().UTC(), Body: body})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.Dir, ".entry-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, c.path(key))
}

func Clear(dir string) error {
	if dir == "" {
		return errors.New("cache directory is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if abs == string(filepath.Separator) || abs == filepath.Clean(os.TempDir()) {
		return fmt.Errorf("refusing to clear broad path %s", abs)
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to clear non-directory cache path %s", abs)
	}
	marker, err := os.ReadFile(filepath.Join(abs, markerName))
	if err != nil || string(marker) != markerContent {
		return fmt.Errorf("refusing to clear %s: ghemails cache marker is missing", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	for _, item := range entries {
		if item.Name() == markerName || entryName.MatchString(item.Name()) || strings.HasPrefix(item.Name(), ".entry-") || strings.HasPrefix(item.Name(), ".marker-") {
			if item.IsDir() {
				continue
			}
			if err := os.Remove(filepath.Join(abs, item.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	// Remove the directory only when it contains no unrelated files.
	remaining, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return os.Remove(abs)
	}
	return nil
}

func ensureDirectory(dir string) error {
	if dir == "" {
		return errors.New("cache directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cache path is not a directory: %s", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	markerPath := filepath.Join(dir, markerName)
	content, readErr := os.ReadFile(markerPath)
	if readErr == nil {
		if string(content) != markerContent {
			return fmt.Errorf("cache marker is invalid: %s", markerPath)
		}
		return nil
	}
	if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	temporary, err := os.CreateTemp(dir, ".marker-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(markerContent); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, markerPath); err != nil {
		content, readErr = os.ReadFile(markerPath)
		if readErr != nil || string(content) != markerContent {
			return err
		}
	}
	return nil
}

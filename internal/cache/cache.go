package cache

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Cache struct {
	Dir string
	TTL time.Duration // 0 means no expiry
}

func New(dir string, ttl time.Duration) *Cache {
	return &Cache{Dir: dir, TTL: ttl}
}

func (c *Cache) Key(parts ...string) string {
	h := md5.Sum([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%x", h)
}

func (c *Cache) Get(key string) ([]byte, bool) {
	path := filepath.Join(c.Dir, key+".json")
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if c.TTL > 0 && time.Since(info.ModTime()) > c.TTL {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

func (c *Cache) Set(key string, data []byte) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(c.Dir, key+".json")
	return os.WriteFile(path, data, 0o644)
}

func (c *Cache) Clear() error {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(c.Dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

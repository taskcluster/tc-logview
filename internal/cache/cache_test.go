package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetGet(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 0)

	key := c.Key("env", "cluster", "filter")
	data := []byte(`[{"test": true}]`)

	if err := c.Set(key, data); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got) != string(data) {
		t.Errorf("got %s, want %s", got, data)
	}
}

func TestGetMiss(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 0)

	_, ok := c.Get("nonexistent")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestTTLExpiry(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 1*time.Second)

	key := c.Key("test")
	if err := c.Set(key, []byte("data")); err != nil {
		t.Fatal(err)
	}

	// Set mtime to 2 seconds ago
	path := filepath.Join(dir, key+".json")
	past := time.Now().Add(-2 * time.Second)
	os.Chtimes(path, past, past)

	_, ok := c.Get(key)
	if ok {
		t.Fatal("expected cache miss due to TTL expiry")
	}
}

func TestTTLNotExpired(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 1*time.Hour)

	key := c.Key("test")
	if err := c.Set(key, []byte("data")); err != nil {
		t.Fatal(err)
	}

	_, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit (TTL not expired)")
	}
}

func TestNoTTL(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 0) // no expiry

	key := c.Key("test")
	if err := c.Set(key, []byte("data")); err != nil {
		t.Fatal(err)
	}

	// Set mtime to 30 days ago
	path := filepath.Join(dir, key+".json")
	past := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(path, past, past)

	_, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit (no TTL)")
	}
}

func TestKeyDeterminism(t *testing.T) {
	c := New("", 0)
	k1 := c.Key("a", "b", "c")
	k2 := c.Key("a", "b", "c")
	if k1 != k2 {
		t.Errorf("keys should be deterministic: %s != %s", k1, k2)
	}

	k3 := c.Key("a", "bc")
	if k1 == k3 {
		t.Error("different inputs should produce different keys")
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 0)

	c.Set(c.Key("a"), []byte("1"))
	c.Set(c.Key("b"), []byte("2"))

	if err := c.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	_, ok := c.Get(c.Key("a"))
	if ok {
		t.Fatal("expected cache empty after Clear")
	}
}

func TestCreatesDirOnSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	c := New(dir, 0)

	if err := c.Set("test", []byte("data")); err != nil {
		t.Fatalf("Set should create dir: %v", err)
	}

	_, ok := c.Get("test")
	if !ok {
		t.Fatal("expected cache hit after Set with dir creation")
	}
}

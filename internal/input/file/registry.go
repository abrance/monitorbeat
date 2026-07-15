// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package file

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// OffsetEntry records the last read offset for a file identified by path+inode.
type OffsetEntry struct {
	Path  string `json:"path"`
	Inode uint64 `json:"inode"`
	Off   int64  `json:"off"`
}

// OffsetRegistry persists file read offsets to a local JSON file.
// Thread-safe for concurrent harvester use.
type OffsetRegistry struct {
	mu   sync.Mutex
	path string
	data map[string]*OffsetEntry // key: "path:inode"
}

// NewOffsetRegistry creates or loads an offset registry at the given file path.
func NewOffsetRegistry(path string) (*OffsetRegistry, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("registry mkdir %s: %w", dir, err)
	}
	r := &OffsetRegistry{
		path: path,
		data: make(map[string]*OffsetEntry),
	}
	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("registry load: %w", err)
	}
	return r, nil
}

func (r *OffsetRegistry) key(path string, inode uint64) string {
	return fmt.Sprintf("%s:%d", path, inode)
}

// Get returns saved offset for path+inode, or 0 if not tracked.
func (r *OffsetRegistry) Get(path string, inode uint64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.data[r.key(path, inode)]
	if !ok {
		return 0
	}
	return e.Off
}

// Set saves offset for path+inode.
func (r *OffsetRegistry) Set(path string, inode uint64, off int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[r.key(path, inode)] = &OffsetEntry{Path: path, Inode: inode, Off: off}
}

// Save writes the registry to disk atomically (write temp + rename).
func (r *OffsetRegistry) Save() error {
	r.mu.Lock()
	entries := make([]*OffsetEntry, 0, len(r.data))
	for _, e := range r.data {
		entries = append(entries, e)
	}
	r.mu.Unlock()

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("registry marshal: %w", err)
	}

	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("registry write: %w", err)
	}
	return os.Rename(tmp, r.path)
}

func (r *OffsetRegistry) load() error {
	b, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	var entries []OffsetEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return fmt.Errorf("registry unmarshal: %w", err)
	}
	for i := range entries {
		e := &entries[i]
		r.data[r.key(e.Path, e.Inode)] = e
	}
	return nil
}

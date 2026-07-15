// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package file

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
)

// HarvesterConfig controls single-file tail behavior.
type HarvesterConfig struct {
	File       string
	Encoding   string // "utf-8" | "gb18030"
	FromBegin  bool
	BufferSize int

	// Registry persists read offsets across restarts. Optional.
	Registry *OffsetRegistry
}

// Harvester tails a single file.
//
// Uses an internal reader goroutine. ReadLine blocks until a line arrives.
// When Registry is set, the harvester restores the last known offset on start
// and periodically saves the current offset.
type Harvester struct {
	cfg    HarvesterConfig
	file   *os.File
	reader *bufio.Reader
	enc    encoding.Encoding

	inode    uint64 // file inode for offset registry key
	offset   int64  // current file position (tracked internally)
	lastSave time.Time

	watcher *fsnotify.Watcher // optional; nil means poll-only fallback

	lineCh chan string
	done   chan struct{}
}

// New opens file and positions reader.
//
// Positioning logic:
//   - FromBegin=true: read from file start
//   - FromBegin=false + Registry has offset for this inode: Seek to saved offset
//   - FromBegin=false + no saved offset: Seek to file end
func New(cfg HarvesterConfig) (*Harvester, error) {
	if cfg.File == "" {
		return nil, errors.New("harvester: empty file path")
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64 * 1024
	}
	enc, err := lookupEncoding(cfg.Encoding)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(cfg.File)
	if err != nil {
		return nil, fmt.Errorf("harvester open %s: %w", cfg.File, err)
	}

	// determine start offset
	var startOff int64
	inode, _ := fileInode(f)
	if cfg.FromBegin {
		startOff = 0
	} else if cfg.Registry != nil {
		startOff = cfg.Registry.Get(cfg.File, inode)
	}

	if startOff > 0 {
		if off, err := f.Seek(startOff, io.SeekStart); err == nil {
			startOff = off
		}
	} else if !cfg.FromBegin {
		if off, err := f.Seek(0, io.SeekEnd); err == nil {
			startOff = off
		}
	}

	var r io.Reader = f
	if enc != nil {
		r = enc.NewDecoder().Reader(f)
	}

	h := &Harvester{
		cfg:      cfg,
		file:     f,
		reader:   bufio.NewReaderSize(r, cfg.BufferSize),
		enc:      enc,
		inode:    inode,
		offset:   startOff,
		lastSave: time.Now(),
		lineCh:   make(chan string, 16),
		done:     make(chan struct{}),
	}

	// try to set up fsnotify watcher (non-fatal on failure)
	if w, err := fsnotify.NewWatcher(); err == nil {
		if err := w.Add(cfg.File); err == nil {
			h.watcher = w
		} else {
			w.Close()
		}
	}

	go h.loop()
	return h, nil
}

func (h *Harvester) loop() {
	defer close(h.lineCh)
	if h.watcher != nil {
		defer h.watcher.Close()
	}

	for {
		if err := h.readOne(); err != nil {
			if errors.Is(err, io.EOF) {
				if err := h.waitMore(); err != nil {
					return // done or watcher error
				}
				continue
			}
			return // real read error
		}
		h.maybeSaveOffset()
	}
}

// readOne reads a single line. Returns io.EOF when no more data available.
func (h *Harvester) readOne() error {
	line, err := h.reader.ReadString('\n')
	if len(line) > 0 {
		if line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		h.lineCh <- line
		h.trackOffset()
	}
	return err
}

// waitMore blocks until new data is available or done is signaled.
// Uses fsnotify for fast wake-up, falling back to 100ms poll.
func (h *Harvester) waitMore() error {
	// reset reader to catch appended data (in case file was rotated/truncated)
	if _, serr := h.file.Seek(0, io.SeekCurrent); serr == nil {
		h.reader = bufio.NewReaderSize(h.underlyingReader(), h.cfg.BufferSize)
	}

	if h.watcher != nil {
		select {
		case <-h.done:
			return errors.New("harvester closed")
		case _, ok := <-h.watcher.Events:
			if !ok {
				return errors.New("watcher closed")
			}
			return nil
		case err, ok := <-h.watcher.Errors:
			if !ok {
				return errors.New("watcher closed")
			}
			// watcher error; fall back to poll
			_ = err
		}
	}

	// poll fallback
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-h.done:
		return errors.New("harvester closed")
	case <-timer.C:
		return nil
	}
}

func (h *Harvester) underlyingReader() io.Reader {
	var r io.Reader = h.file
	if h.enc != nil {
		r = h.enc.NewDecoder().Reader(h.file)
	}
	return r
}

// trackOffset records current file position (best effort).
func (h *Harvester) trackOffset() {
	off, err := h.file.Seek(0, io.SeekCurrent)
	if err == nil {
		h.offset = off
	}
}

// Offset returns the last tracked file position.
func (h *Harvester) Offset() int64 { return h.offset }

// maybeSaveOffset saves offset to registry every 5 seconds.
func (h *Harvester) maybeSaveOffset() {
	if h.cfg.Registry == nil {
		return
	}
	if time.Since(h.lastSave) < 5*time.Second {
		return
	}
	h.cfg.Registry.Set(h.cfg.File, h.inode, h.offset)
	h.lastSave = time.Now()
}

// ReadLine blocks until a line arrives or context is done.
// Returns io.EOF after Close.
func (h *Harvester) ReadLine(ctx context.Context) (string, error) {
	select {
	case line, ok := <-h.lineCh:
		if !ok {
			return "", io.EOF
		}
		return line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close stops the reader goroutine and releases resources.
func (h *Harvester) Close() error {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
	// Final offset save before closing
	if h.cfg.Registry != nil {
		h.cfg.Registry.Set(h.cfg.File, h.inode, h.offset)
	}
	if h.file != nil {
		return h.file.Close()
	}
	return nil
}

func lookupEncoding(name string) (encoding.Encoding, error) {
	switch name {
	case "", "utf-8", "utf8":
		return nil, nil
	case "gb18030":
		return htmlindex.Get("gb18030")
	}
	return nil, fmt.Errorf("harvester: unsupported encoding %q", name)
}

// fileInode returns the inode of an open file, or 0 on failure.
func fileInode(f *os.File) (uint64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("unsupported platform")
	}
	return stat.Ino, nil
}

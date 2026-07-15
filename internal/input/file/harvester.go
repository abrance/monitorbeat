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
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
)

// HarvesterConfig 控制单文件 tail 行为。
type HarvesterConfig struct {
	File       string
	Encoding   string // "utf-8" | "gb18030"
	FromBegin  bool
	BufferSize int
}

// Harvester 单文件 tail reader。
//
// 内部有且只有一个 reader goroutine，所有 ReadLine 调用通过 channel 收结果。
// ctx 取消会让 reader goroutine 在下一次轮询时退出（poll 间隔 100ms）。
// ReadLine 串行调用即可，不要并发。
type Harvester struct {
	cfg    HarvesterConfig
	file   *os.File
	reader *bufio.Reader
	enc    encoding.Encoding

	lineCh chan string
	done   chan struct{}
}

// New 打开文件并按 FromBegin 定位读起点。
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
	if !cfg.FromBegin {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			f.Close()
			return nil, fmt.Errorf("harvester seek end: %w", err)
		}
	}
	var r io.Reader = f
	if enc != nil {
		r = enc.NewDecoder().Reader(f)
	}
	h := &Harvester{
		cfg:    cfg,
		file:   f,
		reader: bufio.NewReaderSize(r, cfg.BufferSize),
		enc:    enc,
		lineCh: make(chan string, 16),
		done:   make(chan struct{}),
	}
	go h.loop()
	return h, nil
}

func (h *Harvester) loop() {
	defer close(h.lineCh)
	for {
		select {
		case <-h.done:
			return
		default:
		}
		line, err := h.reader.ReadString('\n')
		if len(line) > 0 {
			if n := len(line); n > 0 && line[n-1] == '\n' {
				line = line[:n-1]
			}
			h.lineCh <- line
		}
		if err != nil {
			if err != io.EOF {
				return
			}
			select {
			case <-h.done:
				return
			case <-time.After(100 * time.Millisecond):
			}
			if _, serr := h.file.Seek(0, io.SeekCurrent); serr == nil {
				h.reader = bufio.NewReaderSize(h.underlyingReader(), h.cfg.BufferSize)
			}
		}
	}
}

func (h *Harvester) underlyingReader() io.Reader {
	var r io.Reader = h.file
	if h.enc != nil {
		r = h.enc.NewDecoder().Reader(h.file)
	}
	return r
}

// ReadLine 阻塞读一行（不含行尾 \n）；ctx 取消时返回 ctx.Err()，Close 后返回 io.EOF。
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

// Close 通知 reader goroutine 退出并关闭文件。
func (h *Harvester) Close() error {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
	if h.file == nil {
		return nil
	}
	return h.file.Close()
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

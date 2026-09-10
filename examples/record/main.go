// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Record headerless, little-endian EVT3 bytes. Existing files are never overwritten.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"github.com/crosscue/go-imx636"
	"github.com/crosscue/go-imx636/examples/internal/example"
	"io"
	"os"
	"time"
)

func main() { example.Main(run) }
func run(ctx context.Context) (result error) {
	serial := flag.String("serial", "", "camera protocol serial (or USB serial)")
	output := flag.String("out", "capture.evt3", "new output file")
	duration := flag.Duration("duration", 5*time.Second, "capture duration")
	flag.Parse()
	if *duration <= 0 {
		return errors.New("duration must be positive")
	}
	f, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writer := bufio.NewWriterSize(f, 1<<20)
	defer func() { result = errors.Join(result, writer.Flush(), f.Close()) }()
	d, err := imx636.Open(ctx, imx636.Options{Serial: *serial})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, d.Close()) }()
	s, cancel, err := example.Capture(ctx, d, *duration, imx636.StreamOptions{Raw: true})
	defer cancel()
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, s.Close()) }()
	var written uint64
	for {
		p, readErr := s.Next(context.Background())
		if readErr != nil {
			if !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, context.DeadlineExceeded) {
				return readErr
			}
			break
		}
		n, writeErr := writer.Write(p.Raw)
		written += uint64(n)
		if writeErr != nil {
			return writeErr
		}
		if n != len(p.Raw) {
			return io.ErrShortWrite
		}
	}
	if err := s.Close(); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	return example.JSON(map[string]any{"file": *output, "bytes": written, "format": "headerless EVT3 little endian", "camera": d.Info(), "stats": s.Stats()})
}

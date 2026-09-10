// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Consume event packets and report throughput without printing every event.
package main

import (
	"context"
	"errors"
	"flag"
	"github.com/crosscue/go-imx636"
	"github.com/crosscue/go-imx636/examples/internal/example"
	"time"
)

func main() { example.Main(run) }
func run(ctx context.Context) (result error) {
	serial := flag.String("serial", "", "camera protocol serial (or USB serial)")
	duration := flag.Duration("duration", 5*time.Second, "capture duration")
	raw := flag.Bool("raw", false, "benchmark raw USB throughput without decoding")
	flag.Parse()
	if *duration <= 0 {
		return errors.New("duration must be positive")
	}
	d, err := imx636.Open(ctx, imx636.Options{Serial: *serial})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, d.Close()) }()
	s, cancel, err := example.Capture(ctx, d, *duration, imx636.StreamOptions{Raw: *raw})
	defer cancel()
	if err != nil {
		return err
	}
	started := time.Now()
	var events, triggers uint64
	for {
		p, readErr := s.Next(context.Background())
		if readErr != nil {
			if !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, context.DeadlineExceeded) {
				result = readErr
			}
			break
		}
		events += uint64(len(p.Events))
		triggers += uint64(len(p.Triggers))
		// p.Events contains X/Y, polarity, and sensor time in microseconds.
		// Retaining p or its slices is safe; each packet owns its memory.
	}
	result = errors.Join(result, s.Close())
	elapsed := time.Since(started).Seconds()
	stats := s.Stats()
	return errors.Join(result, example.JSON(map[string]any{"events": events, "triggers": triggers, "elapsed_seconds": elapsed, "usb_MB_per_second": float64(stats.USBBytes) / elapsed / 1e6, "events_per_second": float64(events) / elapsed, "stats": stats}))
}

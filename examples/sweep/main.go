// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Measure event rate at several ON-threshold offsets; restore trim on exit.
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
	duration := flag.Duration("duration", time.Second, "measurement duration per point")
	flag.Parse()
	if *duration <= 0 {
		return errors.New("duration must be positive")
	}
	d, err := imx636.Open(ctx, imx636.Options{Serial: *serial})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, d.Close()) }()
	for _, offset := range []int{0, 5, 10} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.ConfigureBiases(ctx, imx636.BiasOffsets{DiffOn: offset}); err != nil {
			return err
		}
		s, cancel, err := example.Capture(ctx, d, *duration, imx636.StreamOptions{})
		if err != nil {
			cancel()
			return err
		}
		start := time.Now()
		var count uint64
		for {
			p, readErr := s.Next(context.Background())
			if readErr != nil {
				if !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, context.DeadlineExceeded) {
					result = readErr
				}
				break
			}
			count += uint64(len(p.Events))
		}
		cancel()
		result = errors.Join(result, s.Close())
		if result != nil {
			return result
		}
		if err := example.JSON(map[string]any{"diff_on_offset": offset, "events": count, "events_per_second": float64(count) / time.Since(start).Seconds(), "readback": d.Biases()}); err != nil {
			return err
		}
	}
	return nil
}

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Package example contains only command-line helpers for the examples.
package example

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/crosscue/go-imx636"
	"os"
	"os/signal"
	"time"
)

func Main(run func(context.Context) error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Capture starts the duration timer after the USB stream has started, so the
// requested measurement time excludes stale-data draining during Start.
func Capture(ctx context.Context, d *imx636.Device, duration time.Duration, options imx636.StreamOptions) (*imx636.Stream, func(), error) {
	capture, cancel := context.WithCancel(ctx)
	s, err := d.Start(capture, options)
	if err != nil {
		cancel()
		return nil, func() {}, err
	}
	timer := time.AfterFunc(duration, cancel)
	return s, func() { timer.Stop(); cancel() }, nil
}
func JSON(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"sync"
	"time"

	"github.com/crosscue/go-imx636"
)

// Events are accumulated on the host between preview ticks. Last polarity wins
// at each pixel. This is an activity display, not an intensity reconstruction.
type accumulator struct {
	mu     sync.Mutex
	pixels *image.Gray
}

func newAccumulator(width, height int) *accumulator {
	a := &accumulator{pixels: image.NewGray(image.Rect(0, 0, width, height))}
	a.clear()
	return a
}

func (a *accumulator) clear() {
	clear(a.pixels.Pix)
}

func (a *accumulator) add(events []imx636.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range events {
		x, y := int(e.X), int(e.Y)
		if x >= a.pixels.Rect.Dx() || y >= a.pixels.Rect.Dy() {
			continue
		}
		value := uint8(128) // OFF events stay visible against the black background.
		if e.Polarity {
			value = 255
		}
		a.pixels.Pix[y*a.pixels.Stride+x] = value
	}
}

func (a *accumulator) snapshot(dst *image.Gray) {
	a.mu.Lock()
	defer a.mu.Unlock()
	copy(dst.Pix, a.pixels.Pix)
	a.clear()
}

// One immutable JPEG is shared by all browsers. A notification carries no
// frames, so a slow client skips to the latest image instead of queuing history.
type latestFrame struct {
	mu      sync.Mutex
	jpeg    []byte
	changed chan struct{}
}

func newLatestFrame() *latestFrame { return &latestFrame{changed: make(chan struct{})} }

func (f *latestFrame) publish(jpeg []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jpeg = jpeg
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *latestFrame) snapshot() ([]byte, <-chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jpeg, f.changed
}

type packetSource interface {
	Next(context.Context) (*imx636.Packet, error)
}

func collectEvents(ctx context.Context, source packetSource, a *accumulator) error {
	for {
		p, err := source.Next(ctx)
		if err != nil {
			return err
		}
		a.add(p.Events)
	}
}

func renderFrames(ctx context.Context, a *accumulator, frames *latestFrame, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pixels := image.NewGray(a.pixels.Rect)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			a.snapshot(pixels)
			// Encoding and all network writes happen outside the accumulator lock.
			var encoded bytes.Buffer
			if err := jpeg.Encode(&encoded, pixels, &jpeg.Options{Quality: 85}); err != nil {
				return err
			}
			frames.publish(encoded.Bytes())
		}
	}
}

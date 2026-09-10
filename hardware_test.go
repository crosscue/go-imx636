//go:build linux && cgo && libusb && integration

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package imx636

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/crosscue/go-imx636/internal/hw"
)

// Explicit opt-in: this test initializes and streams from the selected camera.
func TestHardware(t *testing.T) {
	if os.Getenv("IMX636_HARDWARE") != "1" {
		t.Skip("set IMX636_HARDWARE=1 to enable physical camera tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	devices, err := List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) == 0 {
		t.Fatal("no IDS camera found")
	}
	serial := os.Getenv("IMX636_SERIAL")
	d, err := Open(ctx, Options{Serial: serial})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	baseline := d.baseline
	t.Logf("camera: %s USB serial=%s protocol serial=%s", d.Info().Product, d.Info().USBSerial, d.Info().ProtocolSerial)
	for _, raw := range []bool{false, true, false} {
		capture, stop := context.WithCancel(ctx)
		s, err := d.Start(capture, StreamOptions{Raw: raw})
		if err != nil {
			stop()
			t.Fatal(err)
		}
		timer := time.AfterFunc(time.Second, stop)
		var bytes, events uint64
		for {
			p, err := s.Next(context.Background())
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					t.Error(err)
				}
				break
			}
			bytes += uint64(len(p.Raw))
			events += uint64(len(p.Events))
		}
		timer.Stop()
		stop()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		stats := s.Stats()
		t.Logf("raw=%t raw_bytes=%d events=%d stats=%+v", raw, bytes, events, stats)
		if stats.USBBytes == 0 {
			t.Fatal("no USB data received")
		}
		if !raw && stats.Decoder.InvalidCoordinates != 0 {
			t.Error("invalid event coordinates")
		}
	}
	if err := d.ConfigureBiases(ctx, BiasOffsets{DiffOn: 5}); err != nil {
		t.Fatal(err)
	}
	if got := d.Biases().Biases["diff_on"]; got.EffectiveOffset != 5 {
		t.Fatalf("bias readback=%+v", got)
	}
	temp, tempErr := d.Temperature(ctx)
	light, lightErr := d.Illuminance(ctx)
	t.Logf("temperature=%v (%v); light_raw=%d (%v)", temp, tempErr, light, lightErr)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := hw.Open(ctx, hw.DefaultProduct, d.Info().ProtocolSerial, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for a, want := range baseline {
		v, err := h.ReadRegister(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		if uint8(v) != want {
			t.Errorf("bias 0x%x=%d, expected %d", a, uint8(v), want)
		}
	}
	t.Log("all saved bias values verified after Close")
}

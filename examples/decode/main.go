// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Decode a headerless EVT3 recording; without -in, decode a built-in fixture.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"github.com/crosscue/go-imx636"
	"github.com/crosscue/go-imx636/evt3"
	"github.com/crosscue/go-imx636/examples/internal/example"
	"io"
	"os"
)

func main() { example.Main(run) }
func run(ctx context.Context) error {
	input := flag.String("in", "", "headerless EVT3 file (empty uses a built-in sample)")
	flag.Parse()
	var reader io.Reader = bytes.NewReader([]byte{0x01, 0x80, 0x02, 0x60, 0x03, 0x00, 0x04, 0x28})
	if *input != "" {
		f, err := os.Open(*input)
		if err != nil {
			return err
		}
		defer f.Close()
		reader = f
	}
	d, _ := evt3.NewDecoder(imx636.Width, imx636.Height)
	buffer := make([]byte, 128<<10)
	var events []evt3.Event
	var triggers []evt3.TriggerEvent
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			var err error
			events, triggers, err = d.Decode(buffer[:n], events[:0], triggers[:0])
			if err != nil {
				return err
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			break
		}
	}
	if err := d.Finish(); err != nil {
		return err
	}
	return example.JSON(d.Stats())
}

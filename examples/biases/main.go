// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Set five documented bias offsets, verify readback, and restore on exit.
package main

import (
	"context"
	"errors"
	"flag"
	"github.com/crosscue/go-imx636"
	"github.com/crosscue/go-imx636/examples/internal/example"
)

func main() { example.Main(run) }
func run(ctx context.Context) (result error) {
	serial := flag.String("serial", "", "camera protocol serial (or USB serial)")
	var offsets imx636.BiasOffsets
	flag.IntVar(&offsets.DiffOn, "diff-on", 0, "ON threshold offset")
	flag.IntVar(&offsets.DiffOff, "diff-off", 0, "OFF threshold offset")
	flag.IntVar(&offsets.FO, "fo", 0, "low-pass bandwidth offset")
	flag.IntVar(&offsets.HPF, "hpf", 0, "high-pass filter offset")
	flag.IntVar(&offsets.Refr, "refr", 0, "refractory-period offset")
	flag.Parse()
	if err := imx636.ValidateBiases(offsets); err != nil {
		return err
	}
	d, err := imx636.Open(ctx, imx636.Options{Serial: *serial})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, d.Close()) }()
	if err := d.ConfigureBiases(ctx, offsets); err != nil {
		return err
	}
	return example.JSON(d.Biases())
}

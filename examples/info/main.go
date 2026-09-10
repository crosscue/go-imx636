// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Initialize a camera, inspect its identity, biases and sensor monitors, then close.
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
	flag.Parse()
	d, err := imx636.Open(ctx, imx636.Options{Serial: *serial})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, d.Close()) }()
	temperature, tempErr := d.Temperature(ctx)
	light, lightErr := d.Illuminance(ctx)
	report := map[string]any{"camera": d.Info(), "biases": d.Biases(), "temperature_celsius": temperature, "illuminance_raw": light}
	if tempErr != nil {
		report["temperature_error"] = tempErr.Error()
	}
	if lightErr != nil {
		report["illuminance_error"] = lightErr.Error()
	}
	return example.JSON(report)
}

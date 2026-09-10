// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// View camera activity in a local browser as a live MJPEG event preview.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/crosscue/go-imx636"
	"github.com/crosscue/go-imx636/examples/internal/example"
)

func main() { example.Main(run) }

func run(ctx context.Context) (result error) {
	serial := flag.String("serial", "", "camera protocol serial (or USB serial)")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	fps := flag.Int("fps", 30, "preview frames per second (1..60)")
	flag.Parse()
	if *fps < 1 || *fps > 60 {
		return errors.New("fps must be in 1..60")
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM)
	defer stop()

	// Check the port before initializing hardware.
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	d, err := imx636.Open(ctx, imx636.Options{Serial: *serial})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, d.Close()) }()
	s, err := d.Start(ctx, imx636.StreamOptions{})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Camera view: http://%s\nPress Ctrl+C to stop.\n", listener.Addr())
	return servePreview(ctx, listener, s, time.Second/time.Duration(*fps))
}

//go:build !linux || !cgo || !libusb

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"context"
	"log/slog"
)

type Handle struct{}

func List(context.Context) ([]Info, error) { return nil, ErrUSBUnavailable }
func Open(context.Context, string, string, *slog.Logger) (*Handle, error) {
	return nil, ErrUSBUnavailable
}
func (*Handle) Info(context.Context) (Info, error) { return Info{}, ErrUSBUnavailable }
func (*Handle) Initialize(context.Context, BiasRequest) (InitializationResult, error) {
	return InitializationResult{}, ErrUSBUnavailable
}
func (*Handle) ReadRegister(context.Context, uint32) (uint32, error) { return 0, ErrUSBUnavailable }
func (*Handle) WriteRegister(context.Context, uint32, uint32) error  { return ErrUSBUnavailable }
func (*Handle) Drain(context.Context) error                          { return ErrUSBUnavailable }
func (*Handle) Reader(int, int) (Reader, error)                      { return nil, ErrUSBUnavailable }
func (*Handle) Close() error                                         { return nil }

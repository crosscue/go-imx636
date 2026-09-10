// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"context"
	"errors"
	"strings"
)

var ErrUSBUnavailable = errors.New("USB support unavailable: build on Linux with CGO_ENABLED=1 and -tags libusb; install libusb-1.0 development files")

type Reader interface {
	ReadContext(context.Context, []byte) (int, error)
	Close() error
}

func matchesSerial(info Info, serial string) bool {
	return serial == "" || strings.EqualFold(info.ProtocolSerial, serial) || (info.USBSerial != "" && info.USBSerial == serial)
}

func ApplyBiases(ctx context.Context, w RegisterWriter, baseline map[uint32]uint8, r BiasRequest) (BiasConfiguration, error) {
	var count uint64
	return applyBiasConfiguration(ctx, w, baseline, r, &count)
}

func RestoreBiases(ctx context.Context, w RegisterWriter, baseline map[uint32]uint8) error {
	return restoreBiasAbsolute(ctx, w, baseline)
}

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package imx636

import (
	"context"
	"errors"
	"testing"

	"github.com/crosscue/go-imx636/internal/hw"
)

type openingFake struct {
	*fakeBackend
	info                       Info
	infoErr, initErr, closeErr error
	initialized                int
}

func (b *openingFake) Info(context.Context) (Info, error) { return b.info, b.infoErr }
func (b *openingFake) Initialize(context.Context, hw.BiasRequest) (hw.InitializationResult, error) {
	b.initialized++
	return hw.InitializationResult{}, b.initErr
}
func (b *openingFake) Close() error { b.fakeBackend.Close(); return b.closeErr }

func TestOpenFailureReleasesHandleWithoutExtraRegisterWrites(t *testing.T) {
	infoErr := errors.New("identity read failed")
	initErr := errors.New("baseline read failed")
	closeErr := errors.New("USB close failed")
	for _, tc := range []struct {
		name             string
		info             Info
		infoErr, initErr error
		initializeCalls  int
	}{
		{name: "identity read", infoErr: infoErr},
		{name: "wrong sensor", info: Info{SensorIdentifiers: []string{"another sensor"}}},
		{name: "baseline read", info: Info{SensorIdentifiers: []string{"psee,ccam5_imx636"}}, initErr: initErr, initializeCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &openingFake{fakeBackend: &fakeBackend{registers: make(map[uint32]uint32)}, info: tc.info, infoErr: tc.infoErr, initErr: tc.initErr, closeErr: closeErr}
			d, err := initializeOpenedDevice(context.Background(), b, Options{})
			if d != nil || err == nil || !errors.Is(err, closeErr) {
				t.Fatalf("device=%v error=%v", d, err)
			}
			for _, cause := range []error{tc.infoErr, tc.initErr} {
				if cause != nil && !errors.Is(err, cause) {
					t.Fatalf("lost error %v: %v", cause, err)
				}
			}
			if b.closed != 1 || b.initialized != tc.initializeCalls {
				t.Fatalf("closes=%d initializations=%d", b.closed, b.initialized)
			}
			if len(b.registers) != 0 {
				t.Fatalf("failed Open wrote registers: %v", b.registers)
			}
		})
	}
}

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0
//
// Register initialization sequences and sensor-monitor setup in this file were
// adapted from Neuromorphic Drivers (MIT), Copyright (c) 2020 International
// Centre for Neuromorphic Systems. The complete MIT notice is in
// docs/NEUROMORPHIC-DRIVERS-LICENSE.

package hw

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type registerOperation struct {
	address uint32
	value   uint32
	delay   time.Duration
}

type RegisterWriter interface {
	ReadRegister(context.Context, uint32) (uint32, error)
	WriteRegister(context.Context, uint32, uint32) error
}

type InitializationResult struct {
	Reference                string
	RegisterWrites           uint64
	BiasIDAC                 map[uint32]uint8
	BiasConfiguration        BiasConfiguration
	Verification             map[uint32]uint32
	FinalStoppedVerification map[uint32]uint32
}

var biasAddresses = []uint32{
	0x1000, 0x1004, 0x100c, 0x1010, 0x1014, 0x1018, 0x101c,
	0x1020, 0x1040, 0x1044, 0x1048, 0x104c, 0x1050,
}

const (
	roiStopped      = uint32(1<<1 | 1<<6 | 0x1e000a<<11)
	roiConfigShadow = roiStopped | 1<<5
	roiStarted      = roiStopped | 1<<10
	timeBaseStopped = uint32(1<<2 | 0x64<<4)
	timeBaseStarted = timeBaseStopped | 1
)

// stopSequence mirrors issd_evk3_imx636_stop at pinned source lines 368-398.
var stopSequence = []registerOperation{
	{0x0004, roiStopped, 0},
	{0x002c, 0x0022c324, 0},
	{0x9028, 0x00000002, time.Millisecond},
	{0x9008, timeBaseStopped, 0},
	{0xb000, 0x000002f8, 300 * time.Microsecond},
}

// destroySequence mirrors issd_evk3_imx636_destroy at pinned source lines
// 400-428. Unknown registers retain neutral names instead of guessed meaning.
var destroySequence = []registerOperation{
	{0x0070, 0x00400008, 0},
	{0x006c, 0x0ee47114, 500 * time.Microsecond},
	{0xa00c, 0x00020400, 500 * time.Microsecond},
	{0xa010, 0x00008068, 200 * time.Microsecond},
	{0x1104, 0x00000000, 200 * time.Microsecond},
	{0xa020, 0x00000050, 200 * time.Microsecond},
	{0xa004, 0x000b0500, 200 * time.Microsecond},
	{0xa008, 0x00002404, 200 * time.Microsecond},
	{0xa000, 0x000b0500, 0},
	{0xb044, 0x00000000, 0},
	{0xb004, 0x0000000a, 0},
	{0xb040, 0x0000000e, 0},
	{0xb0c8, 0x00000000, 0},
	{0xb040, 0x00000006, 0},
	{0xb040, 0x00000004, 0},
	{0x0000, 0x4f006442, 0},
	{0x0000, 0x0f006442, 0},
	{0x00b8, 0x00000401, 0},
	{0x00b8, 0x00000400, 0},
	{0xb07c, 0x00000000, 0},
}

// baseInitializationSequence mirrors the pinned IMX636 init table at source
// lines 430-546. The exact ordered values and delays are intentionally data.
var baseInitializationSequence = []registerOperation{
	{0x001c, 0x00000001, 0},
	{0x400004, 0x00000001, time.Second},
	{0x400004, 0x00000000, 500 * time.Millisecond},
	{0xb000, 0x00000158, time.Second},
	{0xb044, 0x00000000, 300 * time.Microsecond},
	{0xb004, 0x0000000a, 0},
	{0xb040, 0x00000000, 0},
	{0xb0c8, 0x00000000, 0},
	{0xb040, 0x00000000, 0},
	{0xb040, 0x00000000, 0},
	{0x0000, 0x4f006442, 0},
	{0x0000, 0x0f006442, 0},
	{0x00b8, 0x00000400, 0},
	{0x00b8, 0x00000400, 0},
	{0xb07c, 0x00000000, 0},
	{0xb074, 0x00000002, 0},
	{0xb078, 0x000000a0, 0},
	{0x00c0, 0x00000110, 0},
	{0x00c0, 0x00000210, 0},
	{0xb120, 0x00000001, 0},
	{0xe120, 0x00000000, 0},
	{0xb068, 0x00000004, 0},
	{0xb07c, 0x00000001, 10 * time.Microsecond},
	{0xb07c, 0x00000003, time.Millisecond},
	{0x00b8, 0x00000401, 0},
	{0x00b8, 0x00000409, 0},
	{0x0000, 0x4f006442, 0},
	{0x0000, 0x4f00644a, 0},
	{0xb080, 0x00000077, 0},
	{0xb084, 0x0000000f, 0},
	{0xb088, 0x00000037, 0},
	{0xb08c, 0x00000037, 0},
	{0xb090, 0x000000df, 0},
	{0xb094, 0x00000057, 0},
	{0xb098, 0x00000037, 0},
	{0xb09c, 0x00000067, 0},
	{0xb0a0, 0x00000037, 0},
	{0xb0a4, 0x0000002f, 0},
	{0xb0ac, 0x00000028, 0},
	{0xb0cc, 0x00000001, 0},
	{0xb000, 0x000002f8, 0},
	{0xb004, 0x0000008a, 0},
	{0xb01c, 0x00000030, 0},
	{0xb020, 0x00002000, 0},
	{0xb02c, 0x000000ff, 0},
	{0xb030, 0x00003e80, 0},
	{0xb028, 0x00000fa0, 0},
	{0xa000, 0x000b0501, 200 * time.Microsecond},
	{0xa008, 0x00002405, 200 * time.Microsecond},
	{0xa004, 0x000b0501, 200 * time.Microsecond},
	{0xa020, 0x00000150, 200 * time.Microsecond},
	{0xb040, 0x00000007, 0},
	{0xb064, 0x00000006, 0},
	{0xb040, 0x0000000f, 100 * time.Microsecond},
	{0xb004, 0x0000008a, 200 * time.Microsecond},
	{0xb0c8, 0x00000003, 200 * time.Microsecond},
	{0xb044, 0x00000001, 0},
	{0xb000, 0x000002f9, 0},
	{0x7008, 0x00000001, 0},
	{0x7000, 0x00070001, 0},
	{0x8000, 0x0001e085, 0},
	{0x9008, timeBaseStopped, 0},
	{0x0004, roiStopped, 0},
	{0x0018, 0x00000200, 0},
	{0x1014, biasRegisterValue(0x4d, 0x50), 0},
	{0x9004, 0x00000000, time.Millisecond},
	{0x9000, 0x00000200, 0},
}

// monitorAndFilterSequence mirrors pinned source lines 548-706. These blocks
// are initialized into their known bypassed/monitoring baseline for first
// physical bring-up; no user-facing controls are exposed.
var monitorAndFilterSequence = []registerOperation{
	{0x004c, adcControl(false), 0},
	{0x004c, adcControl(true), 0},
	{0x0054, 1<<1 | 0x210<<2, 100 * time.Microsecond},
	{0x005c, 1<<1 | 0x80020<<2, 0},
	{0x005c, 1 | 1<<1 | 0x80020<<2, 100 * time.Microsecond},
	{0x004c, adcControl(false), 0},
	{0x004c, adcControl(true), 0},
	{0x0054, 1<<1 | 0x84<<2 | 1<<12, 0},
	{0x0074, 0x00000002, 10 * time.Microsecond},
	{0x0074, 0x00000003, 20 * time.Microsecond},
	{0x000c, 0x00000001, 5 * time.Microsecond},
	{0x000c, 0x00000003, 0},
	{0x000c, 0x00000007, 0},
	{0xc008, 15 | 156<<8 | 8<<16, 0},
	{0xc000, 1 | 1<<2, 0},
	{0xd00c, 13 | 1<<5 | 1<<9, 0},
	{0xd004, 1480<<1 | 1<<24, 0},
	{0xd008, 100000 << 1, 0},
	{0xd0c0, 4 | 280<<12 | 10<<24, 0},
	{0xd0c4, 0, 0},
	{0xd000, 1 | 1<<2, 0},
	{0x6000, 0x00155400, 0},
	{0x6004, 0x00000000, 0},
	{0x6028, 0x00000002, 0},
}

var startSequence = []registerOperation{
	{0xb000, 0x000002f9, 0},
	{0x9028, 0x00000000, 0},
	{0x9008, timeBaseStarted, 0},
	{0x002c, 0x0022c724, 0},
	{0x0004, roiStarted, 0},
}

func biasRegisterValue(idac, vdac uint32) uint32 {
	return idac | vdac<<8 | 1<<16 | 1<<21 | 1<<23 | 1<<24 | 1<<28
}

func adcControl(clock bool) uint32 {
	value := uint32(1 | 0xec8<<3)
	if clock {
		value |= 1 << 1
	}
	return value
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runRegisterSequence(ctx context.Context, writer RegisterWriter, operations []registerOperation, count *uint64) error {
	for _, operation := range operations {
		if err := writer.WriteRegister(ctx, operation.address, operation.value); err != nil {
			return fmt.Errorf("write register 0x%08x=0x%08x: %w", operation.address, operation.value, err)
		}
		*count++
		if err := sleepContext(ctx, operation.delay); err != nil {
			return err
		}
	}
	return nil
}

func readBiases(ctx context.Context, writer RegisterWriter) (map[uint32]uint8, error) {
	biases := make(map[uint32]uint8, len(biasAddresses))
	for _, address := range biasAddresses {
		value, err := writer.ReadRegister(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("read bias register 0x%08x: %w", address, err)
		}
		biases[address] = uint8(value)
	}
	return biases, nil
}

func InitializeSensor(ctx context.Context, writer RegisterWriter) (result InitializationResult, resultErr error) {
	return initializeSensor(ctx, writer, nil, BiasRequest{Profile: "default"})
}

func InitializeSensorConfigured(ctx context.Context, writer RegisterWriter, request BiasRequest) (result InitializationResult, resultErr error) {
	return initializeSensor(ctx, writer, nil, request)
}

func initializeSensor(ctx context.Context, writer RegisterWriter, drain func(context.Context) error, request BiasRequest) (result InitializationResult, resultErr error) {
	if err := ValidateBiasOffsets(request.Offsets); err != nil {
		return result, err
	}
	result.Reference = InitializationID
	biases, err := readBiases(ctx, writer)
	if err != nil {
		return result, err
	}
	result.BiasIDAC = biases
	if _, err := calculateBiasConfiguration(biases, request); err != nil {
		return result, err
	}
	defer func() {
		if resultErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, StopSensor(cleanup, writer), restoreBiasAbsolute(cleanup, writer, biases))
		}
	}()
	for _, sequence := range [][]registerOperation{stopSequence, destroySequence, baseInitializationSequence, monitorAndFilterSequence} {
		if err := runRegisterSequence(ctx, writer, sequence, &result.RegisterWrites); err != nil {
			return result, err
		}
	}
	// Event-rate controller lookup initialization, pinned lines 743-772.
	if err := writer.WriteRegister(ctx, 0x602c, 1); err != nil {
		return result, err
	}
	result.RegisterWrites++
	for offset := uint32(0); offset < 230; offset++ {
		if err := writer.WriteRegister(ctx, 0x6800+offset*4, 0x08080808); err != nil {
			return result, fmt.Errorf("initialize ERC drop LUT %d: %w", offset, err)
		}
		result.RegisterWrites++
	}
	if err := writer.WriteRegister(ctx, 0x602c, 2); err != nil {
		return result, err
	}
	result.RegisterWrites++
	for offset := uint32(0); offset < 256; offset++ {
		value := ((offset*2 + 1) << 16) | offset*2
		if err := writer.WriteRegister(ctx, 0x6400+offset*4, value); err != nil {
			return result, fmt.Errorf("initialize time-drop LUT %d: %w", offset, err)
		}
		result.RegisterWrites++
	}
	for _, operation := range []registerOperation{
		{0x6050, 0, 0}, {0x6060, 0, 0}, {0x6070, 0, 0},
		{0x6000, 0x00155401, 0},
		// external_trigger=1 matches the pinned default but capture remains free-running.
		{0x7004, 0x1ff | 1<<10 | 0x18<<11, 0},
	} {
		if err := runRegisterSequence(ctx, writer, []registerOperation{operation}, &result.RegisterWrites); err != nil {
			return result, err
		}
	}
	if drain != nil {
		if err := drain(ctx); err != nil {
			return result, fmt.Errorf("drain stale event data: %w", err)
		}
	}
	if err := applyDefaultConfiguration(ctx, writer, biases, &result.RegisterWrites); err != nil {
		return result, err
	}
	result.BiasConfiguration, err = applyBiasConfiguration(ctx, writer, biases, request, &result.RegisterWrites)
	if err != nil {
		return result, err
	}
	result.Verification = make(map[uint32]uint32)
	for _, address := range []uint32{0x0004, 0x9008, 0x9028, 0xb000} {
		value, err := writer.ReadRegister(ctx, address)
		if err != nil {
			return result, fmt.Errorf("verify initialized register 0x%08x: %w", address, err)
		}
		result.Verification[address] = value
	}
	return result, nil
}

func applyDefaultConfiguration(ctx context.Context, writer RegisterWriter, biases map[uint32]uint8, count *uint64) error {
	for _, address := range biasAddresses {
		if err := writer.WriteRegister(ctx, address, biasRegisterValue(uint32(biases[address]), 0)); err != nil {
			return fmt.Errorf("restore bias register 0x%08x: %w", address, err)
		}
		*count++
	}
	for offset := uint32(0); offset < 40; offset++ {
		if err := writer.WriteRegister(ctx, 0x2000+offset*4, 0); err != nil {
			return err
		}
		*count++
	}
	for offset := uint32(0); offset < 23; offset++ {
		value := uint32(0)
		if offset == 22 {
			value = 0x00ff0000
		}
		if err := writer.WriteRegister(ctx, 0x4000+offset*4, value); err != nil {
			return err
		}
		*count++
	}
	if err := writer.WriteRegister(ctx, 0x0004, roiConfigShadow); err != nil {
		return err
	}
	*count++
	for offset := uint32(0); offset < 64; offset++ {
		if err := writer.WriteRegister(ctx, 0x9100+offset*4, 0); err != nil {
			return err
		}
		*count++
	}
	return nil
}

func StartSensor(ctx context.Context, writer RegisterWriter) error {
	var count uint64
	return runRegisterSequence(ctx, writer, startSequence, &count)
}

func StopSensor(ctx context.Context, writer RegisterWriter) error {
	var count uint64
	return runRegisterSequence(ctx, writer, stopSequence, &count)
}

func DestroySensor(ctx context.Context, writer RegisterWriter) error {
	var count uint64
	return runRegisterSequence(ctx, writer, destroySequence, &count)
}

func CleanupSensor(ctx context.Context, writer RegisterWriter) error {
	return errors.Join(StopSensor(ctx, writer), DestroySensor(ctx, writer))
}

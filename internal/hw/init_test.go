// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryRegisters struct {
	values  map[uint32]uint32
	writes  []registerOperation
	readErr error
}

func (m *memoryRegisters) ReadRegister(_ context.Context, address uint32) (uint32, error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return m.values[address], nil
}

func (m *memoryRegisters) WriteRegister(_ context.Context, address, value uint32) error {
	if m.values == nil {
		m.values = make(map[uint32]uint32)
	}
	m.values[address] = value
	m.writes = append(m.writes, registerOperation{address: address, value: value})
	return nil
}

func TestKnownSequencePacking(t *testing.T) {
	if roiStopped != 0xf0005042 || roiConfigShadow != 0xf0005062 || roiStarted != 0xf0005442 {
		t.Fatalf("roi values stopped=%08x shadow=%08x started=%08x", roiStopped, roiConfigShadow, roiStarted)
	}
	if timeBaseStopped != 0x644 || timeBaseStarted != 0x645 {
		t.Fatalf("timebase values %x %x", timeBaseStopped, timeBaseStarted)
	}
	if got := biasRegisterValue(0x4d, 0x50); got != 0x11a1504d {
		t.Fatalf("bias packed value %08x", got)
	}
}

func TestInitializeSensorSequenceAndPreservedBiases(t *testing.T) {
	registers := &memoryRegisters{values: make(map[uint32]uint32)}
	for i, address := range biasAddresses {
		registers.values[address] = uint32(0x40 + i)
	}
	result, err := InitializeSensor(context.Background(), registers)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reference != InitializationID || result.RegisterWrites < 700 || uint64(len(registers.writes)) != result.RegisterWrites {
		t.Fatalf("result=%+v writes=%d", result, len(registers.writes))
	}
	for i, address := range biasAddresses {
		want := biasRegisterValue(uint32(0x40+i), 0)
		if got := registers.values[address]; got != want {
			t.Fatalf("bias 0x%08x=%08x want %08x", address, got, want)
		}
	}
	if registers.values[0x6800+229*4] != 0x08080808 {
		t.Fatal("ERC LUT was not fully initialized")
	}
	if registers.values[0x6400+255*4] != (511<<16)|510 {
		t.Fatal("time-drop LUT was not fully initialized")
	}
	if registers.values[0x4000+22*4] != 0x00ff0000 || registers.values[0x9100+63*4] != 0 {
		t.Fatal("default masks were not fully initialized")
	}
}

func TestInitializeDoesNotWriteWhenBiasReadFails(t *testing.T) {
	registers := &memoryRegisters{values: make(map[uint32]uint32), readErr: errors.New("read failed")}
	if _, err := InitializeSensor(context.Background(), registers); err == nil {
		t.Fatal("accepted failed pre-write bias read")
	}
	if len(registers.writes) != 0 {
		t.Fatalf("wrote %d registers after failed identity read", len(registers.writes))
	}
}

func TestIMX636BiasOffsetsUseFactoryValuesAndVerifyReadback(t *testing.T) {
	registers := &memoryRegisters{values: make(map[uint32]uint32)}
	for i, address := range biasAddresses {
		registers.values[address] = uint32(40 + i)
	}
	request := BiasRequest{Profile: "test", Offsets: BiasOffsets{DiffOn: -10, DiffOff: 20, FO: 5, HPF: 10, Refr: 30}}
	result, err := InitializeSensorConfigured(context.Background(), registers, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range imx636BiasSpecs {
		value := result.BiasConfiguration.Biases[spec.name]
		want := int(result.BiasIDAC[spec.address]) + spec.offset(request.Offsets)
		if value.TargetAbsolute != want || value.ReadbackAbsolute != want || value.EffectiveOffset != spec.offset(request.Offsets) {
			t.Fatalf("%s configuration=%+v want target/readback=%d", spec.name, value, want)
		}
	}
	if got := uint8(registers.values[0x1014]); got != result.BiasIDAC[0x1014] {
		t.Fatalf("bias_diff changed from factory %d to %d", result.BiasIDAC[0x1014], got)
	}
}

type mismatchedReadback struct{ memoryRegisters }

func (m *mismatchedReadback) ReadRegister(ctx context.Context, address uint32) (uint32, error) {
	value, err := m.memoryRegisters.ReadRegister(ctx, address)
	if address == 0x1010 && len(m.writes) != 0 {
		return value + 1, err
	}
	return value, err
}

func TestBiasReadbackMismatchIsRejectedAndRestored(t *testing.T) {
	registers := &mismatchedReadback{memoryRegisters{values: make(map[uint32]uint32)}}
	for _, address := range biasAddresses {
		registers.values[address] = 100
	}
	_, err := InitializeSensorConfigured(context.Background(), registers, BiasRequest{Offsets: BiasOffsets{DiffOn: 1}})
	if err == nil || !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatalf("error=%v, want readback mismatch", err)
	}
	if got := uint8(registers.values[0x1010]); got != 100 {
		t.Fatalf("partial bias apply was not restored: %d", got)
	}
}

func TestBiasValidationAndUnknownName(t *testing.T) {
	if err := ValidateBiasOffsets(BiasOffsets{DiffOn: -86}); err == nil || !strings.Contains(err.Error(), "-85..140") {
		t.Fatalf("range error=%v", err)
	}
	var offsets BiasOffsets
	if err := SetBiasOffset(&offsets, "foo", 0); err == nil || !strings.Contains(err.Error(), "unknown XCP bias") {
		t.Fatalf("unknown bias error=%v", err)
	}
}

func TestStartAndStopState(t *testing.T) {
	registers := &memoryRegisters{values: make(map[uint32]uint32)}
	if err := StartSensor(context.Background(), registers); err != nil {
		t.Fatal(err)
	}
	if registers.values[0x0004] != roiStarted || registers.values[0x9008] != timeBaseStarted || registers.values[0x9028] != 0 {
		t.Fatalf("started state %+v", registers.values)
	}
	if err := StopSensor(context.Background(), registers); err != nil {
		t.Fatal(err)
	}
	if registers.values[0x0004] != roiStopped || registers.values[0x9008] != timeBaseStopped || registers.values[0x9028] != 2 || registers.values[0xb000] != 0x2f8 {
		t.Fatalf("stopped state %+v", registers.values)
	}
}

// A rejected offset must leave the sensor untouched even when it is within
// nominal limits but cannot be represented relative to this device's trim.
func TestInitializeRejectsAbsoluteBiasBeforeWriting(t *testing.T) {
	registers := &memoryRegisters{values: make(map[uint32]uint32)}
	for _, address := range biasAddresses {
		registers.values[address] = 250
	}
	_, err := InitializeSensorConfigured(context.Background(), registers, BiasRequest{Offsets: BiasOffsets{DiffOn: 10}})
	if err == nil {
		t.Fatal("accepted out-of-range absolute IDAC")
	}
	if len(registers.writes) != 0 {
		t.Fatalf("wrote %d registers before validation", len(registers.writes))
	}
}

type failedInitialization struct {
	memoryRegisters
	cause  error
	failed bool
}

func (m *failedInitialization) WriteRegister(ctx context.Context, address, value uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !m.failed {
		m.failed = true
		// A write may reach hardware even when its acknowledgement fails.
		m.values[0x1010] = 200
		return m.cause
	}
	return m.memoryRegisters.WriteRegister(ctx, address, value)
}

func TestInitializationFailureRestoresSavedBiases(t *testing.T) {
	cause := errors.New("lost acknowledgement")
	registers := &failedInitialization{memoryRegisters: memoryRegisters{values: make(map[uint32]uint32)}, cause: cause}
	for _, address := range biasAddresses {
		registers.values[address] = 100
	}
	_, err := InitializeSensor(context.Background(), registers)
	if !errors.Is(err, cause) {
		t.Fatalf("lost failure: %v", err)
	}
	for _, address := range biasAddresses {
		if uint8(registers.values[address]) != 100 {
			t.Fatalf("bias %x not restored", address)
		}
	}
	if registers.values[0x9008] != timeBaseStopped {
		t.Fatal("sensor not stopped during rollback")
	}
}

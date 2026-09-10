// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// BiasOffsets are signed IMX636 offsets relative to this sensor's own
// factory-trimmed IDAC values. They are never raw register values.
type BiasOffsets struct {
	DiffOn  int `json:"diff_on" yaml:"diff_on"`
	DiffOff int `json:"diff_off" yaml:"diff_off"`
	FO      int `json:"fo" yaml:"fo"`
	HPF     int `json:"hpf" yaml:"hpf"`
	Refr    int `json:"refr" yaml:"refr"`
}

// BiasOffsetPatch preserves whether a value was explicitly supplied. This is
// used for profile/config/CLI precedence; an explicit zero is meaningful.
type BiasOffsetPatch struct {
	DiffOn  *int `json:"diff_on,omitempty" yaml:"diff_on,omitempty"`
	DiffOff *int `json:"diff_off,omitempty" yaml:"diff_off,omitempty"`
	FO      *int `json:"fo,omitempty" yaml:"fo,omitempty"`
	HPF     *int `json:"hpf,omitempty" yaml:"hpf,omitempty"`
	Refr    *int `json:"refr,omitempty" yaml:"refr,omitempty"`
}

type BiasRequest struct {
	Profile string      `json:"profile"`
	Offsets BiasOffsets `json:"offsets"`
}

type BiasValue struct {
	FactoryAbsolute  int `json:"factory"`
	RequestedOffset  int `json:"requested_offset"`
	TargetAbsolute   int `json:"target_absolute"`
	AppliedAbsolute  int `json:"applied_absolute"`
	ReadbackAbsolute int `json:"readback_absolute"`
	EffectiveOffset  int `json:"effective_offset"`
	MinimumOffset    int `json:"minimum_offset"`
	MaximumOffset    int `json:"maximum_offset"`
}

type BiasConfiguration struct {
	Profile string               `json:"profile"`
	Biases  map[string]BiasValue `json:"biases"`
}

type biasSpec struct {
	name      string
	address   uint32
	minimum   int
	maximum   int
	offset    func(BiasOffsets) int
	setOffset func(*BiasOffsets, int)
}

var imx636BiasSpecs = []biasSpec{
	{name: "diff_on", address: 0x1010, minimum: -85, maximum: 140, offset: func(v BiasOffsets) int { return v.DiffOn }, setOffset: func(v *BiasOffsets, n int) { v.DiffOn = n }},
	{name: "diff_off", address: 0x1018, minimum: -35, maximum: 190, offset: func(v BiasOffsets) int { return v.DiffOff }, setOffset: func(v *BiasOffsets, n int) { v.DiffOff = n }},
	{name: "fo", address: 0x1004, minimum: -35, maximum: 55, offset: func(v BiasOffsets) int { return v.FO }, setOffset: func(v *BiasOffsets, n int) { v.FO = n }},
	{name: "hpf", address: 0x100c, minimum: 0, maximum: 120, offset: func(v BiasOffsets) int { return v.HPF }, setOffset: func(v *BiasOffsets, n int) { v.HPF = n }},
	{name: "refr", address: 0x1020, minimum: -20, maximum: 235, offset: func(v BiasOffsets) int { return v.Refr }, setOffset: func(v *BiasOffsets, n int) { v.Refr = n }},
}

func BiasNames() []string {
	names := make([]string, 0, len(imx636BiasSpecs))
	for _, spec := range imx636BiasSpecs {
		names = append(names, spec.name)
	}
	return names
}

func BiasRange(name string) (minimum, maximum int, ok bool) {
	spec, ok := findBiasSpec(name)
	if !ok {
		return 0, 0, false
	}
	return spec.minimum, spec.maximum, true
}

func ValidateBiasOffsets(offsets BiasOffsets) error {
	for _, spec := range imx636BiasSpecs {
		value := spec.offset(offsets)
		if value < spec.minimum || value > spec.maximum {
			return fmt.Errorf("xcp bias %s offset %d is outside supported range %d..%d", spec.name, value, spec.minimum, spec.maximum)
		}
	}
	return nil
}

func SetBiasOffset(offsets *BiasOffsets, name string, value int) error {
	spec, ok := findBiasSpec(name)
	if !ok {
		return fmt.Errorf("unknown XCP bias: %s", name)
	}
	candidate := *offsets
	spec.setOffset(&candidate, value)
	if err := ValidateBiasOffsets(candidate); err != nil {
		return err
	}
	*offsets = candidate
	return nil
}

func ApplyBiasPatch(offsets *BiasOffsets, patch BiasOffsetPatch) {
	if patch.DiffOn != nil {
		offsets.DiffOn = *patch.DiffOn
	}
	if patch.DiffOff != nil {
		offsets.DiffOff = *patch.DiffOff
	}
	if patch.FO != nil {
		offsets.FO = *patch.FO
	}
	if patch.HPF != nil {
		offsets.HPF = *patch.HPF
	}
	if patch.Refr != nil {
		offsets.Refr = *patch.Refr
	}
}

func findBiasSpec(name string) (biasSpec, bool) {
	for _, spec := range imx636BiasSpecs {
		if spec.name == name {
			return spec, true
		}
	}
	return biasSpec{}, false
}

func calculateBiasConfiguration(factory map[uint32]uint8, request BiasRequest) (BiasConfiguration, error) {
	if err := ValidateBiasOffsets(request.Offsets); err != nil {
		return BiasConfiguration{}, err
	}
	configuration := BiasConfiguration{Profile: request.Profile, Biases: make(map[string]BiasValue, len(imx636BiasSpecs))}
	for _, spec := range imx636BiasSpecs {
		factoryValue, ok := factory[spec.address]
		if !ok {
			return BiasConfiguration{}, fmt.Errorf("camera does not expose expected IMX636 bias %s at register 0x%08x", spec.name, spec.address)
		}
		offset := spec.offset(request.Offsets)
		target := int(factoryValue) + offset
		if target < 0 || target > 255 {
			return BiasConfiguration{}, fmt.Errorf("xcp bias %s factory value %d plus offset %d produces invalid raw IDAC value %d", spec.name, factoryValue, offset, target)
		}
		configuration.Biases[spec.name] = BiasValue{
			FactoryAbsolute: int(factoryValue), RequestedOffset: offset, TargetAbsolute: target,
			MinimumOffset: spec.minimum, MaximumOffset: spec.maximum,
		}
	}
	return configuration, nil
}

func applyBiasConfiguration(ctx context.Context, writer RegisterWriter, factory map[uint32]uint8, request BiasRequest, count *uint64) (BiasConfiguration, error) {
	configuration, err := calculateBiasConfiguration(factory, request)
	if err != nil {
		return BiasConfiguration{}, err
	}
	applied := make([]biasSpec, 0, len(imx636BiasSpecs))
	restore := func() error {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var restoreErr error
		for i := len(applied) - 1; i >= 0; i-- {
			spec := applied[i]
			restoreErr = errors.Join(restoreErr, writer.WriteRegister(restoreCtx, spec.address, biasRegisterValue(uint32(factory[spec.address]), 0)))
		}
		return restoreErr
	}
	for _, spec := range imx636BiasSpecs {
		value := configuration.Biases[spec.name]
		applied = append(applied, spec)
		if err := writer.WriteRegister(ctx, spec.address, biasRegisterValue(uint32(value.TargetAbsolute), 0)); err != nil {
			return BiasConfiguration{}, errors.Join(fmt.Errorf("write xcp bias %s target %d: %w", spec.name, value.TargetAbsolute, err), restore())
		}
		*count++
		readback, err := writer.ReadRegister(ctx, spec.address)
		if err != nil {
			return BiasConfiguration{}, errors.Join(fmt.Errorf("read back xcp bias %s: %w", spec.name, err), restore())
		}
		value.ReadbackAbsolute = int(uint8(readback))
		value.AppliedAbsolute = value.ReadbackAbsolute
		value.EffectiveOffset = value.ReadbackAbsolute - value.FactoryAbsolute
		configuration.Biases[spec.name] = value
		if value.ReadbackAbsolute != value.TargetAbsolute {
			return BiasConfiguration{}, errors.Join(fmt.Errorf("xcp bias %s readback mismatch: requested absolute %d, read back %d", spec.name, value.TargetAbsolute, value.ReadbackAbsolute), restore())
		}
	}
	return configuration, nil
}

func restoreBiasAbsolute(ctx context.Context, writer RegisterWriter, values map[uint32]uint8) error {
	addresses := make([]int, 0, len(values))
	for address := range values {
		addresses = append(addresses, int(address))
	}
	sort.Ints(addresses)
	var result error
	for _, rawAddress := range addresses {
		address := uint32(rawAddress)
		if err := writer.WriteRegister(ctx, address, biasRegisterValue(uint32(values[address]), 0)); err != nil {
			result = errors.Join(result, fmt.Errorf("restore bias register 0x%08x: %w", address, err))
			continue
		}
		readback, err := writer.ReadRegister(ctx, address)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("verify restored bias register 0x%08x: %w", address, err))
		} else if uint8(readback) != values[address] {
			result = errors.Join(result, fmt.Errorf("restored bias register 0x%08x readback %d, want %d", address, uint8(readback), values[address]))
		}
	}
	return result
}

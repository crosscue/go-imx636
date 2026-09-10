//go:build linux && cgo && libusb

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"testing"

	"github.com/google/gousb"
)

// Synthetic USB descriptors: these tests never enumerate or open a device.
func testUSBConfig(number int) gousb.ConfigDesc {
	return gousb.ConfigDesc{Number: number, Interfaces: []gousb.InterfaceDesc{{Number: 3,
		AltSettings: []gousb.InterfaceSetting{{Number: 3, Alternate: 1, Class: 0xff, SubClass: 0x19,
			Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{
				0x02: {Address: 0x02, TransferType: gousb.TransferTypeBulk},
				0x81: {Address: 0x81, TransferType: gousb.TransferTypeBulk},
				0x82: {Address: 0x82, TransferType: gousb.TransferTypeBulk},
			},
		}},
	}}}
}

func TestTopologyUsesActiveConfiguration(t *testing.T) {
	desc := &gousb.DeviceDesc{Configs: map[int]gousb.ConfigDesc{2: testUSBConfig(2), 1: testUSBConfig(1)}}
	selected, ok := selectTopologyInConfig(desc, 2)
	if !ok || selected.configuration != 2 || selected.interfaceNumber != 3 || selected.alternate != 1 {
		t.Fatalf("topology=%+v found=%t", selected, ok)
	}
	if _, ok := selectTopologyInConfig(desc, 99); ok {
		t.Fatal("fell back from absent active configuration")
	}
	selected, ok = selectTopology(desc)
	if !ok || selected.configuration != 1 {
		t.Fatal("enumeration did not choose lowest compatible configuration")
	}
}

func TestTopologyRejectsIncompatibleDescriptors(t *testing.T) {
	for _, kind := range []string{"class", "subclass", "missing command", "missing response", "missing events", "interrupt events"} {
		t.Run(kind, func(t *testing.T) {
			config := testUSBConfig(1)
			alt := &config.Interfaces[0].AltSettings[0]
			switch kind {
			case "class":
				alt.Class = 0
			case "subclass":
				alt.SubClass = 0
			case "missing command":
				delete(alt.Endpoints, 0x02)
			case "missing response":
				delete(alt.Endpoints, 0x82)
			case "missing events":
				delete(alt.Endpoints, 0x81)
			case "interrupt events":
				endpoint := alt.Endpoints[0x81]
				endpoint.TransferType = gousb.TransferTypeInterrupt
				alt.Endpoints[0x81] = endpoint
			}
			desc := &gousb.DeviceDesc{Configs: map[int]gousb.ConfigDesc{1: config}}
			if _, ok := selectTopology(desc); ok {
				t.Fatal("accepted incompatible interface")
			}
		})
	}
}

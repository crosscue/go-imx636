// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import "testing"

func TestProtocolSerialSelectionWithoutUSBSerial(t *testing.T) {
	info := Info{ProtocolSerial: "DEADBEEF"}
	if !matchesSerial(info, "") || !matchesSerial(info, "deadbeef") {
		t.Fatal("protocol serial was not matched")
	}
	if matchesSerial(info, "other") {
		t.Fatal("unrelated camera matched")
	}
	info.USBSerial = "USB-123"
	if !matchesSerial(info, "USB-123") {
		t.Fatal("USB serial was not matched")
	}
}

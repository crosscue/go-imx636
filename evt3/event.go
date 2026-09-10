// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package evt3

// Event is a decoded contrast-detection (CD) polarity event. CameraTime is the
// reconstructed EVT3 camera timestamp in microseconds; it is not host or UTC
// time. Polarity is true for ON (increasing brightness), false for OFF.
// X and Y retain sensor coordinates without display transformations.
type Event struct {
	CameraTime uint64
	X          uint16
	Y          uint16
	Polarity   bool
}

// TriggerEvent is an external-trigger edge, kept separate from CD events.
// CameraTime uses the same sensor microseconds as Event. Edge is true for
// rising, false for falling; ID is the encoded trigger channel (0..15).
type TriggerEvent struct {
	CameraTime uint64
	ID         uint8
	Edge       bool
}

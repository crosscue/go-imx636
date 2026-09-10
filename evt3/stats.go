// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package evt3

// Stats contains cumulative decoder accounting. Decode never requires callers
// to scrape logs to inspect data-quality or protocol anomalies.
type Stats struct {
	InputBytes uint64
	Words      uint64
	WordTypes  [16]uint64

	CDEvents  uint64
	OnEvents  uint64
	OffEvents uint64

	TriggerEvents uint64

	InvalidCoordinates uint64
	ReservedWords      uint64
	UnsupportedWords   uint64
	MalformedWords     uint64

	TimestampWraps     uint64
	TimestampBackwards uint64
	FirstTimestampUS   uint64
	LastTimestampUS    uint64
	HasTimestamp       bool

	FirstTimestampAnomalyWord        uint64
	FirstTimestampAnomalyType        uint8
	FirstTimestampAnomalyPreviousUS  uint64
	FirstTimestampAnomalyCandidateUS uint64
}

// State is a read-only snapshot of the protocol state needed to continue a
// stream. It is primarily useful for diagnostics and chunk-invariance tests.
type State struct {
	CurrentY     uint16
	BaseX        uint32
	Polarity     bool
	TimeLow      uint16
	TimeHigh     uint16
	CameraTimeUS uint64
	Epoch        uint64

	HasY           bool
	HasVectorBase  bool
	HasTimeHigh    bool
	HasPendingByte bool
	PendingByte    byte
}

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Package evt3 decodes stateful EVT 3.0 byte streams independently of their
// transport. Input is the IMX636 default little-endian representation.
package evt3

import (
	"errors"
	"fmt"
)

const (
	wordTypeAddrY       = 0x0
	wordTypeAddrX       = 0x2
	wordTypeVectBaseX   = 0x3
	wordTypeVect12      = 0x4
	wordTypeVect8       = 0x5
	wordTypeTimeLow     = 0x6
	wordTypeContinued4  = 0x7
	wordTypeTimeHigh    = 0x8
	wordTypeTrigger     = 0xa
	wordTypeOthers      = 0xe
	wordTypeContinued12 = 0xf

	wordPayloadMask   = uint16(0x0fff)
	coordinateMask    = uint16(0x07ff)
	polarityMask      = uint16(0x0800)
	timeHighWrap      = uint64(1 << 24)
	timeHighHalfRange = uint16(1 << 11)
)

// ErrTruncatedWord reports a stream ending with an unpaired byte. An odd-sized
// Decode call is valid; this error is returned only when Finish establishes
// that no following chunk exists.
var ErrTruncatedWord = errors.New("odd/truncated final EVT3 byte")

// Decoder preserves all state required to decode arbitrary byte chunks.
// Construct it with NewDecoder. Its methods must not be called concurrently.
type Decoder struct {
	width  uint16
	height uint16

	currentY    uint16
	baseX       uint32
	polarity    bool
	timeLow     uint16
	timeHigh    uint16
	currentTime uint64
	epoch       uint64

	hasY           bool
	hasVectorBase  bool
	hasTimeHigh    bool
	hasPendingByte bool
	pendingByte    byte

	stats Stats
}

// NewDecoder constructs a little-endian EVT3 decoder for the supplied sensor
// geometry.
func NewDecoder(width, height uint16) (*Decoder, error) {
	if width == 0 || height == 0 || width > 2048 || height > 2048 {
		return nil, fmt.Errorf("invalid EVT3 geometry %dx%d", width, height)
	}
	return &Decoder{width: width, height: height}, nil
}

// Reset clears protocol state and cumulative statistics while retaining the
// configured geometry.
func (d *Decoder) Reset() {
	width, height := d.width, d.height
	*d = Decoder{width: width, height: height}
}

// Stats returns cumulative decoder statistics.
func (d *Decoder) Stats() Stats { return d.stats }

// State returns a snapshot of the state that crosses Decode call boundaries.
func (d *Decoder) State() State {
	return State{
		CurrentY: d.currentY, BaseX: d.baseX, Polarity: d.polarity,
		TimeLow: d.timeLow, TimeHigh: d.timeHigh, CameraTimeUS: d.currentTime,
		Epoch: d.epoch, HasY: d.hasY, HasVectorBase: d.hasVectorBase,
		HasTimeHigh: d.hasTimeHigh, HasPendingByte: d.hasPendingByte,
		PendingByte: d.pendingByte,
	}
}

// Decode consumes arbitrary little-endian EVT3 bytes. Returned slices reuse
// the caller-provided storage when it has sufficient capacity; the decoder
// itself performs no per-event allocation. Decode appends to the destinations;
// pass dst[:0] to reuse storage without retaining earlier events. Input bytes
// are not retained. Each output slice preserves wire order, but separating
// triggers from CD events loses their interleaving.
func (d *Decoder) Decode(data []byte, dst []Event, triggerDst []TriggerEvent) ([]Event, []TriggerEvent, error) {
	d.stats.InputBytes += uint64(len(data))

	if d.hasPendingByte && len(data) != 0 {
		word := uint16(d.pendingByte) | uint16(data[0])<<8
		d.hasPendingByte = false
		d.pendingByte = 0
		d.processWord(word, &dst, &triggerDst)
		data = data[1:]
	}

	i := 0
	for ; i+1 < len(data); i += 2 {
		word := uint16(data[i]) | uint16(data[i+1])<<8
		d.processWord(word, &dst, &triggerDst)
	}
	if i != len(data) {
		d.pendingByte = data[i]
		d.hasPendingByte = true
	}
	return dst, triggerDst, nil
}

// Finish validates end-of-stream framing. It does not reset the decoder.
func (d *Decoder) Finish() error {
	if d.hasPendingByte {
		return ErrTruncatedWord
	}
	return nil
}

func (d *Decoder) processWord(word uint16, events *[]Event, triggers *[]TriggerEvent) {
	typ := uint8(word >> 12)
	d.stats.Words++
	d.stats.WordTypes[typ]++

	switch typ {
	case wordTypeAddrY:
		d.currentY = word & coordinateMask
		d.hasY = true

	case wordTypeAddrX:
		x := uint32(word & coordinateMask)
		polarity := word&polarityMask != 0
		if !d.hasY || !d.hasTimeHigh {
			d.stats.MalformedWords++
			return
		}
		d.emitCD(x, polarity, events)

	case wordTypeVectBaseX:
		d.baseX = uint32(word & coordinateMask)
		d.polarity = word&polarityMask != 0
		d.hasVectorBase = true

	case wordTypeVect12:
		d.expandVector(word&wordPayloadMask, 12, events)

	case wordTypeVect8:
		if word&0x0f00 != 0 {
			d.stats.MalformedWords++
		}
		d.expandVector(word&0x00ff, 8, events)

	case wordTypeTimeLow:
		d.updateTimeLow(word & wordPayloadMask)

	case wordTypeTimeHigh:
		d.updateTimeHigh(word & wordPayloadMask)

	case wordTypeTrigger:
		if !d.hasTimeHigh {
			d.stats.MalformedWords++
			return
		}
		d.stats.TriggerEvents++
		d.noteTimestamp(d.currentTime)
		if triggers != nil {
			*triggers = append(*triggers, TriggerEvent{
				CameraTime: d.currentTime,
				ID:         uint8((word >> 8) & 0x0f),
				Edge:       word&1 != 0,
			})
		}

	case wordTypeContinued4, wordTypeOthers, wordTypeContinued12:
		// These are documented extension mechanisms, but their subtype
		// semantics are outside CD/trigger decoding. Count, never reinterpret.
		d.stats.UnsupportedWords++

	default:
		// 0x1, 0x9, 0xb, 0xc and 0xd are reserved by EVT3.
		d.stats.ReservedWords++
	}
}

func (d *Decoder) expandVector(mask uint16, width uint32, events *[]Event) {
	if !d.hasY || !d.hasVectorBase || !d.hasTimeHigh {
		d.stats.MalformedWords++
		return
	}
	for bit := uint32(0); bit < width; bit++ {
		if mask&(uint16(1)<<bit) != 0 {
			// Compare before adding: malformed streams can drive baseX to its limit.
			if d.baseX >= uint32(d.width) {
				d.stats.InvalidCoordinates++
			} else {
				d.emitCD(d.baseX+bit, d.polarity, events)
			}
		}
	}
	// EVT3 advances across unset bits too; a following vector continues at
	// the next 12- or 8-pixel block.
	// Saturate rather than wrapping back into valid sensor coordinates.
	if d.baseX > ^uint32(0)-width {
		d.baseX = ^uint32(0)
	} else {
		d.baseX += width
	}
}

func (d *Decoder) emitCD(x uint32, polarity bool, events *[]Event) {
	if x >= uint32(d.width) || d.currentY >= d.height {
		d.stats.InvalidCoordinates++
		return
	}
	d.stats.CDEvents++
	if polarity {
		d.stats.OnEvents++
	} else {
		d.stats.OffEvents++
	}
	d.noteTimestamp(d.currentTime)
	if events != nil {
		*events = append(*events, Event{
			CameraTime: d.currentTime,
			X:          uint16(x),
			Y:          d.currentY,
			Polarity:   polarity,
		})
	}
}

func (d *Decoder) updateTimeHigh(value uint16) {
	if !d.hasTimeHigh {
		d.hasTimeHigh = true
		d.timeHigh = value
		d.timeLow = 0
		d.currentTime = uint64(value) << 12
		return
	}
	if value == d.timeHigh {
		return // EVT3 deliberately repeats TIME_HIGH for resynchronization.
	}

	if value < d.timeHigh {
		if d.timeHigh-value > timeHighHalfRange {
			d.epoch++
			d.stats.TimestampWraps++
		} else {
			// A small backwards TIME_HIGH change is not a plausible 24-bit
			// wrap. Keep the last good timebase so emitted time stays monotonic.
			d.noteTimestampBackwards(wordTypeTimeHigh, d.epoch*timeHighWrap+uint64(value)<<12)
			return
		}
	}

	d.timeHigh = value
	d.timeLow = 0
	candidate := d.epoch*timeHighWrap + uint64(value)<<12
	if candidate < d.currentTime {
		d.noteTimestampBackwards(wordTypeTimeHigh, candidate)
		return
	}
	d.currentTime = candidate
}

func (d *Decoder) updateTimeLow(value uint16) {
	d.timeLow = value
	if !d.hasTimeHigh {
		// TIME_LOW cannot establish the 24-bit epoch by itself.
		d.stats.MalformedWords++
		return
	}
	candidate := d.epoch*timeHighWrap + uint64(d.timeHigh)<<12 + uint64(value)
	if candidate < d.currentTime {
		// Multiple EVT3 sources may report non-monotonic low words within a
		// high period. Count the anomaly and clamp emitted camera time.
		d.noteTimestampBackwards(wordTypeTimeLow, candidate)
		return
	}
	d.currentTime = candidate
}

func (d *Decoder) noteTimestamp(value uint64) {
	if !d.stats.HasTimestamp {
		d.stats.HasTimestamp = true
		d.stats.FirstTimestampUS = value
	}
	d.stats.LastTimestampUS = value
}

func (d *Decoder) noteTimestampBackwards(typ uint8, candidate uint64) {
	d.stats.TimestampBackwards++
	if d.stats.TimestampBackwards == 1 {
		d.stats.FirstTimestampAnomalyWord = d.stats.Words
		d.stats.FirstTimestampAnomalyType = typ
		d.stats.FirstTimestampAnomalyPreviousUS = d.currentTime
		d.stats.FirstTimestampAnomalyCandidateUS = candidate
	}
}

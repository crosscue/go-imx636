// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package evt3

import (
	"errors"
	"reflect"
	"testing"
)

func TestDecodeSingleEventsAndStateChanges(t *testing.T) {
	data := words(
		0x8123, // TIME_HIGH: 0x123
		0x6456, // TIME_LOW: 0x456
		0x0007, // Y: 7
		0x2809, // X: 9, ON
		0x200a, // X: 10, OFF
		0x0008, // Y: 8
		0x2814, // X: 20, ON
	)
	d := mustDecoder(t, 1280, 720)
	events, triggers, err := d.Decode(data, make([]Event, 0, 3), make([]TriggerEvent, 0, 1))
	if err != nil || len(triggers) != 0 {
		t.Fatalf("Decode error=%v triggers=%v", err, triggers)
	}
	wantTime := uint64(0x123456)
	want := []Event{
		{CameraTime: wantTime, X: 9, Y: 7, Polarity: true},
		{CameraTime: wantTime, X: 10, Y: 7, Polarity: false},
		{CameraTime: wantTime, X: 20, Y: 8, Polarity: true},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	stats := d.Stats()
	if stats.CDEvents != 3 || stats.OnEvents != 2 || stats.OffEvents != 1 || stats.FirstTimestampUS != wantTime || stats.LastTimestampUS != wantTime {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestDecodeAcceptsNilDestinationSlices(t *testing.T) {
	d := mustDecoder(t, 1280, 720)
	events, triggers, err := d.Decode(words(0x8000, 0x6001, 0x0002, 0x2803, 0xa101), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0] != (Event{CameraTime: 1, X: 3, Y: 2, Polarity: true}) {
		t.Fatalf("unexpected events: %#v", events)
	}
	if len(triggers) != 1 || triggers[0] != (TriggerEvent{CameraTime: 1, ID: 1, Edge: true}) {
		t.Fatalf("unexpected triggers: %#v", triggers)
	}
}

func TestDecodeVectorsBitOrderProgressionAndPolarity(t *testing.T) {
	data := words(
		0x8000, // TIME_HIGH
		0x6064, // 100 us
		0x0005, // Y: 5
		0x380a, // base X: 10, ON
		0x4805, // VECT_12 bits 0, 2, 11
		0x5081, // VECT_8 bits 0, 7; base advanced from 10 to 22
		0x301e, // base X: 30, OFF
		0x4003, // VECT_12 bits 0, 1
	)
	d := mustDecoder(t, 1280, 720)
	events, _, err := d.Decode(data, make([]Event, 0, 7), make([]TriggerEvent, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{CameraTime: 100, X: 10, Y: 5, Polarity: true},
		{CameraTime: 100, X: 12, Y: 5, Polarity: true},
		{CameraTime: 100, X: 21, Y: 5, Polarity: true},
		{CameraTime: 100, X: 22, Y: 5, Polarity: true},
		{CameraTime: 100, X: 29, Y: 5, Polarity: true},
		{CameraTime: 100, X: 30, Y: 5, Polarity: false},
		{CameraTime: 100, X: 31, Y: 5, Polarity: false},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if got := d.State().BaseX; got != 42 {
		t.Fatalf("base X = %d, want 42", got)
	}
}

func TestTimestampReconstructionWrapAndAnomaly(t *testing.T) {
	t.Run("wrap", func(t *testing.T) {
		data := words(
			0x8fff, 0x6ffe, 0x0001, 0x2002,
			0x8000, 0x6005, 0x2003,
		)
		d := mustDecoder(t, 1280, 720)
		events, _, _ := d.Decode(data, make([]Event, 0, 2), make([]TriggerEvent, 0))
		want := []Event{
			{CameraTime: 0x00fffffe, X: 2, Y: 1},
			{CameraTime: 0x01000005, X: 3, Y: 1},
		}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %#v, want %#v", events, want)
		}
		if s := d.Stats(); s.TimestampWraps != 1 || s.TimestampBackwards != 0 {
			t.Fatalf("unexpected timestamp stats: %+v", s)
		}
	})

	t.Run("small backwards changes are counted and clamped", func(t *testing.T) {
		d := mustDecoder(t, 1280, 720)
		events, _, _ := d.Decode(words(0x8005, 0x600a, 0x0001, 0x2001, 0x8004, 0x6009, 0x2002), make([]Event, 0, 2), make([]TriggerEvent, 0))
		if events[1].CameraTime != events[0].CameraTime {
			t.Fatalf("camera time moved backwards: %#v", events)
		}
		if got := d.Stats().TimestampBackwards; got != 2 {
			t.Fatalf("backwards count = %d, want 2", got)
		}
		s := d.Stats()
		if s.FirstTimestampAnomalyWord != 5 || s.FirstTimestampAnomalyType != wordTypeTimeHigh || s.FirstTimestampAnomalyPreviousUS != 0x500a || s.FirstTimestampAnomalyCandidateUS != 0x4000 {
			t.Fatalf("unexpected first anomaly detail: %+v", s)
		}
	})
}

func TestTriggerReservedUnsupportedAndMalformed(t *testing.T) {
	d := mustDecoder(t, 1280, 720)
	data := words(
		0x2001, // X before Y/time
		0x6001, // TIME_LOW before TIME_HIGH
		0x4001, // vector before Y/base/time
		0x1000, // reserved type
		0x7000, // unsupported CONTINUED_4
		0xe014, // unsupported OTHERS
		0xf000, // unsupported CONTINUED_12
		0x8002, 0x6003, 0x0004,
		0x3fff, // base 2047, ON
		0x5101, // malformed VECT_8 unused bit plus valid bit 0
		0xa301, // trigger ID 3, rising
	)
	events, triggers, err := d.Decode(data, make([]Event, 0, 1), make([]TriggerEvent, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("unexpected valid events: %#v", events)
	}
	wantTrigger := []TriggerEvent{{CameraTime: 0x2003, ID: 3, Edge: true}}
	if !reflect.DeepEqual(triggers, wantTrigger) {
		t.Fatalf("triggers = %#v, want %#v", triggers, wantTrigger)
	}
	s := d.Stats()
	if s.MalformedWords != 4 || s.ReservedWords != 1 || s.UnsupportedWords != 3 || s.InvalidCoordinates != 1 || s.TriggerEvents != 1 {
		t.Fatalf("unexpected stats: %+v", s)
	}
}

func TestCoordinateBoundaries(t *testing.T) {
	d := mustDecoder(t, 1280, 720)
	data := words(
		0x8000, 0x6001,
		0x02cf, // Y: 719
		0x24ff, // X: 1279, valid
		0x2500, // X: 1280, invalid
		0x34fe, // vector base: 1278, OFF
		0x4007, // x 1278/1279 valid, x 1280 invalid
		0x02d0, // Y: 720
		0x2000, // invalid Y
	)
	events, _, _ := d.Decode(data, make([]Event, 0, 3), make([]TriggerEvent, 0))
	want := []Event{
		{CameraTime: 1, X: 1279, Y: 719},
		{CameraTime: 1, X: 1278, Y: 719},
		{CameraTime: 1, X: 1279, Y: 719},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if got := d.Stats().InvalidCoordinates; got != 3 {
		t.Fatalf("invalid coordinates = %d, want 3", got)
	}
}

func TestOddChunksFinishAndReset(t *testing.T) {
	d := mustDecoder(t, 1280, 720)
	if _, _, err := d.Decode([]byte{0x34}, make([]Event, 0), make([]TriggerEvent, 0)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(d.Finish(), ErrTruncatedWord) {
		t.Fatalf("Finish error = %v, want ErrTruncatedWord", d.Finish())
	}
	if _, _, err := d.Decode([]byte{0x12}, make([]Event, 0), make([]TriggerEvent, 0)); err != nil {
		t.Fatal(err)
	}
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}
	if s := d.Stats(); s.InputBytes != 2 || s.Words != 1 || s.WordTypes[1] != 1 {
		t.Fatalf("unexpected stats after completing word: %+v", s)
	}
	d.Reset()
	if d.Stats() != (Stats{}) || d.State() != (State{}) {
		t.Fatalf("Reset did not clear state: stats=%+v state=%+v", d.Stats(), d.State())
	}
}

func TestChunkBoundaryInvariance(t *testing.T) {
	fixtures := map[string][]byte{
		"single":              words(0x8123, 0x6456, 0x0007, 0x2809, 0xa201),
		"vectors":             words(0x8000, 0x6001, 0x0005, 0x380a, 0x4805, 0x5081, 0x301e, 0x4003),
		"wrap-and-extensions": words(0x8fff, 0x6ffe, 0x0001, 0x2002, 0xe001, 0xf123, 0x8000, 0x6005, 0x2803),
	}
	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			want := decodeChunks(t, data, []int{len(data)})
			strategies := [][]int{
				{1},
				{2},
				{1, 7, 2, 5, 3},
				{9, 1, 4, 3},
			}
			for _, chunks := range strategies {
				got := decodeChunks(t, data, chunks)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("chunks %v differ\ngot:  %#v\nwant: %#v", chunks, got, want)
				}
			}
			for split := 0; split <= len(data); split++ {
				got := decodeChunks(t, data, []int{split, len(data) - split})
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("split at byte %d differs\ngot:  %#v\nwant: %#v", split, got, want)
				}
			}
		})
	}
}

type decodeOutcome struct {
	Events   []Event
	Triggers []TriggerEvent
	Stats    Stats
	State    State
}

func decodeChunks(t *testing.T, data []byte, sizes []int) decodeOutcome {
	t.Helper()
	d := mustDecoder(t, 1280, 720)
	events := make([]Event, 0, 64)
	triggers := make([]TriggerEvent, 0, 4)
	offset, sizeIndex := 0, 0
	for offset < len(data) {
		size := sizes[sizeIndex%len(sizes)]
		sizeIndex++
		if size == 0 {
			continue
		}
		if size > len(data)-offset {
			size = len(data) - offset
		}
		var err error
		events, triggers, err = d.Decode(data[offset:offset+size], events, triggers)
		if err != nil {
			t.Fatal(err)
		}
		offset += size
	}
	if err := d.Finish(); err != nil {
		t.Fatal(err)
	}
	return decodeOutcome{Events: events, Triggers: triggers, Stats: d.Stats(), State: d.State()}
}

func BenchmarkDecodeEVT3(b *testing.B) {
	pattern := words(0x8001, 0x6010, 0x000a, 0x3800, 0x4fff, 0x50ff, 0x2801, 0x2002)
	data := make([]byte, 1<<20)
	for offset := 0; offset < len(data); offset += len(pattern) {
		copy(data[offset:], pattern)
	}
	d := mustDecoder(b, 1280, 720)
	events := make([]Event, 0, (len(data)/len(pattern))*22)
	triggers := make([]TriggerEvent, 0, 1)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Reset()
		out, _, err := d.Decode(data, events[:0], triggers[:0])
		if err != nil {
			b.Fatal(err)
		}
		if len(out) == 0 {
			b.Fatal("decoder produced no events")
		}
	}
}

type decoderTesting interface {
	Helper()
	Fatalf(string, ...any)
}

func mustDecoder(t decoderTesting, width, height uint16) *Decoder {
	t.Helper()
	d, err := NewDecoder(width, height)
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	return d
}

func words(values ...uint16) []byte {
	data := make([]byte, 0, len(values)*2)
	for _, value := range values {
		data = append(data, byte(value), byte(value>>8))
	}
	return data
}

// Seed the state near overflow to exercise a long malformed vector sequence
// without decoding hundreds of megabytes in a unit test.
func TestVectorCoordinateOverflowAndRecovery(t *testing.T) {
	d := mustDecoder(t, 1280, 720)
	d.Decode(words(0x8000, 0x0001, 0x3000), nil, nil)
	d.baseX = ^uint32(0) - 3
	events, _, err := d.Decode(words(0x4fff, 0x50ff, 0x4fff), nil, nil)
	if err != nil || len(events) != 0 || d.Stats().InvalidCoordinates != 32 {
		t.Fatalf("overflow emitted events or lost accounting: events=%v stats=%+v err=%v", events, d.Stats(), err)
	}
	events, _, err = d.Decode(words(0x3002, 0x4001), nil, nil)
	if err != nil || len(events) != 1 || events[0].X != 2 {
		t.Fatalf("new vector base did not recover: %v, %v", events, err)
	}
}

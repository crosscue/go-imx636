// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package evt3

import (
	"errors"
	"reflect"
	"testing"
)

func FuzzDecoderChunking(f *testing.F) {
	f.Add([]byte{1, 128, 2, 96, 3, 0, 4, 40}, uint16(3))
	f.Add([]byte{255, 143, 255, 111, 0, 0, 0, 56, 255, 79, 255, 95, 0, 128, 1, 160}, uint16(1))
	f.Add([]byte{1}, uint16(7))
	f.Fuzz(func(t *testing.T, data []byte, size uint16) {
		if len(data) > 8192 {
			t.Skip()
		}
		chunk := int(size)%1024 + 1
		a, _ := NewDecoder(1280, 720)
		b, _ := NewDecoder(1280, 720)
		full, fullTriggers, err := a.Decode(data, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		var events []Event
		var triggers []TriggerEvent
		for offset := 0; offset < len(data); {
			end := min(offset+chunk, len(data))
			events, triggers, err = b.Decode(data[offset:end], events, triggers)
			if err != nil {
				t.Fatal(err)
			}
			offset = end
		}
		if !reflect.DeepEqual(full, events) || !reflect.DeepEqual(fullTriggers, triggers) || a.Stats() != b.Stats() || a.State() != b.State() {
			t.Fatal("chunk boundaries changed decoding")
		}
		if errors.Is(a.Finish(), ErrTruncatedWord) != errors.Is(b.Finish(), ErrTruncatedWord) {
			t.Fatal("chunk boundaries changed final framing")
		}
	})
}

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package imx636

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/crosscue/go-imx636/evt3"
	"github.com/crosscue/go-imx636/internal/hw"
)

// Event is a polarity event with sensor coordinates and microsecond time.
type Event = evt3.Event

// TriggerEvent records an external edge separately from polarity events.
type TriggerEvent = evt3.TriggerEvent

// StreamOptions bounds memory and controls decoding. Zero values use defaults.
// Raw skips decoding and returns the exact EVT3 payload, without a file header.
type StreamOptions struct {
	Raw        bool
	BufferSize int
	Transfers  int
	QueueDepth int
}

func (o StreamOptions) normalized() (StreamOptions, error) {
	if o.BufferSize == 0 {
		o.BufferSize = 128 << 10
	}
	if o.Transfers == 0 {
		o.Transfers = 8
	}
	if o.QueueDepth == 0 {
		o.QueueDepth = 8
	}
	if o.BufferSize < 1024 || o.BufferSize > 4<<20 || o.BufferSize%1024 != 0 {
		return o, errors.New("buffer size must be a multiple of 1024 in 1024..4194304")
	}
	if o.Transfers < 1 || o.Transfers > 64 {
		return o, errors.New("transfers must be in 1..64")
	}
	if o.QueueDepth < 1 || o.QueueDepth > 256 {
		return o, errors.New("queue depth must be in 1..256")
	}
	// Worst-case vector expansion is up to 12 events per 16-bit word.
	expansion := 1
	if !o.Raw {
		expansion = 96
	}
	if int64(o.BufferSize)*(int64(o.Transfers)+int64(o.QueueDepth+2)*int64(expansion)) > 512<<20 {
		return o, errors.New("stream configuration exceeds the 512 MiB estimated memory budget")
	}
	return o, nil
}

// Packet owns its slices: they remain valid after Next, Stop, and Close.
// HostTime is the host receipt time, distinct from sensor microsecond timestamps.
// In raw mode only Raw and HostTime are populated. Decoded mode can return
// empty event slices when the input contains timestamps or other EVT3 words.
type Packet struct {
	HostTime time.Time
	Raw      []byte
	Events   []Event
	Triggers []TriggerEvent
}

// StreamStats is a cumulative session snapshot. QueueHighWater is sampled
// after sends and may undercount occupancy when a consumer runs concurrently.
type StreamStats struct {
	USBBytes       uint64
	USBReads       uint64
	Packets        uint64
	QueueHighWater int
	Decoder        evt3.Stats
}

// Stream is a single-consumer stream created by Device.Start; its zero value
// is not usable. Do not copy a Stream. Next must not be called concurrently.
// Stats, Err, and Close may be used concurrently with Next.
type Stream struct {
	device     *Device
	cancel     context.CancelFunc
	done       chan struct{}
	packets    chan *Packet
	mu         sync.Mutex
	stats      StreamStats
	err        error
	cleanupErr error // written before done closes
}

// Start begins acquisition. Its context controls the entire session. Call Stop
// before restarting, including after the context expires or a read fails.
func (d *Device) Start(ctx context.Context, options StreamOptions) (*Stream, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, ErrClosed
	}
	if d.active != nil {
		return nil, ErrStreaming
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.transport.Drain(ctx); err != nil {
		return nil, fmt.Errorf("drain stale events: %w", err)
	}
	reader, err := d.transport.Reader(options.BufferSize, options.Transfers)
	if err != nil {
		return nil, err
	}
	if err := hw.StartSensor(ctx, d.transport); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return nil, errors.Join(err, reader.Close(), hw.StopSensor(cleanup, d.transport))
	}
	session, cancel := context.WithCancel(ctx)
	s := &Stream{device: d, cancel: cancel, done: make(chan struct{}), packets: make(chan *Packet, options.QueueDepth)}
	d.active = s
	go s.run(session, reader, options)
	return s, nil
}

func (s *Stream) run(ctx context.Context, reader hw.Reader, options StreamOptions) {
	var result error
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.cleanupErr = errors.Join(reader.Close(), hw.StopSensor(cleanup, s.device.transport))
		s.mu.Lock()
		s.err = errors.Join(result, s.cleanupErr)
		s.mu.Unlock()
		s.cancel()
		close(s.packets)
		close(s.done)
	}()
	decoder, _ := evt3.NewDecoder(Width, Height)
	buffer := make([]byte, options.BufferSize)
	for {
		if err := ctx.Err(); err != nil {
			result = err
			return
		}
		n, err := reader.ReadContext(ctx, buffer)
		if n < 0 || n > len(buffer) {
			result = errors.New("USB reader returned an invalid byte count")
			return
		}
		if n > 0 {
			p := &Packet{HostTime: time.Now()}
			if options.Raw {
				p.Raw = append([]byte(nil), buffer[:n]...)
			} else {
				p.Events, p.Triggers, result = decoder.Decode(buffer[:n], nil, nil)
				if result != nil {
					return
				}
			}
			s.mu.Lock()
			s.stats.USBBytes += uint64(n)
			s.stats.USBReads++
			s.stats.Decoder = decoder.Stats()
			s.mu.Unlock()
			select {
			case s.packets <- p:
				s.mu.Lock()
				s.stats.Packets++
				if q := len(s.packets); q > s.stats.QueueHighWater {
					s.stats.QueueHighWater = q
				}
				s.mu.Unlock()
			case <-ctx.Done():
				result = ctx.Err()
				return
			}
		}
		if err != nil {
			result = err
			if ctx.Err() != nil {
				result = ctx.Err()
			} else if !options.Raw {
				result = errors.Join(err, decoder.Finish())
			}
			return
		}
	}
}

// Next returns the next packet, draining queued packets before the terminal
// stream error. A canceled Next context only cancels that call, not acquisition.
func (s *Stream) Next(ctx context.Context) (*Packet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case p, ok := <-s.packets:
		if ok {
			return p, nil
		}
		if err := s.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stats returns a synchronized copy of cumulative session counters.
func (s *Stream) Stats() StreamStats { s.mu.Lock(); defer s.mu.Unlock(); return s.stats }

// Err returns nil until acquisition and cleanup finish.
func (s *Stream) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// Close stops this stream. It cannot stop a later stream on the same camera.
func (s *Stream) Close() error {
	d := s.device
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.active == s {
		return d.stopLocked()
	}
	<-s.done
	return s.cleanupErr
}

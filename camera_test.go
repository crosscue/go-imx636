// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package imx636

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/crosscue/go-imx636/evt3"
	"github.com/crosscue/go-imx636/internal/hw"
)

type readResult struct {
	data []byte
	err  error
}
type fakeReader struct {
	input  chan readResult
	closed int
}

func (r *fakeReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	select {
	case next := <-r.input:
		return copy(p, next.data), next.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
func (r *fakeReader) Close() error { r.closed++; return nil }

type fakeBackend struct {
	mu        sync.Mutex
	registers map[uint32]uint32
	reader    *fakeReader
	closed    int
	failStart bool
}

func (b *fakeBackend) ReadRegister(_ context.Context, a uint32) (uint32, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.registers[a], nil
}
func (b *fakeBackend) WriteRegister(ctx context.Context, a, v uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failStart && a == 0x9008 && v == 0x645 {
		return errors.New("start failed")
	}
	b.registers[a] = v
	return nil
}
func (b *fakeBackend) Drain(ctx context.Context) error    { return ctx.Err() }
func (b *fakeBackend) Reader(int, int) (hw.Reader, error) { return b.reader, nil }
func (b *fakeBackend) Close() error                       { b.closed++; return nil }
func testDevice() (*Device, *fakeBackend) {
	b := &fakeBackend{registers: make(map[uint32]uint32), reader: &fakeReader{input: make(chan readResult, 32)}}
	baseline := map[uint32]uint8{}
	for _, a := range []uint32{0x1010, 0x1018, 0x1004, 0x100c, 0x1020} {
		baseline[a] = 100
		b.registers[a] = 100
	}
	return &Device{transport: b, baseline: baseline}, b
}
func words(values ...uint16) []byte {
	b := make([]byte, len(values)*2)
	for i, v := range values {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	return b
}
func next(t *testing.T, s *Stream) *Packet {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStreamDecodesAcrossUSBChunksAndOwnsPackets(t *testing.T) {
	d, b := testDevice()
	defer d.Close()
	s, err := d.Start(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data := words(0x8001, 0x6002, 0x0003, 0x2804)
	b.reader.input <- readResult{data: data[:3]}
	b.reader.input <- readResult{data: data[3:]}
	next(t, s)
	p := next(t, s)
	want := []Event{{CameraTime: 4098, X: 4, Y: 3, Polarity: true}}
	if !reflect.DeepEqual(p.Events, want) {
		t.Fatalf("events=%+v", p.Events)
	}
	b.reader.input <- readResult{data: words(0x2005)}
	next(t, s)
	if !reflect.DeepEqual(p.Events, want) {
		t.Fatal("subsequent read mutated retained packet")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if b.reader.closed != 1 {
		t.Fatalf("reader closed %d times", b.reader.closed)
	}
	if s.Stats().Decoder.CDEvents != 2 {
		t.Fatal(s.Stats())
	}
}

func TestStopUnblocksFullQueueAndAllowsRestart(t *testing.T) {
	d, b := testDevice()
	defer d.Close()
	s, err := d.Start(context.Background(), StreamOptions{Raw: true, QueueDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		b.reader.input <- readResult{data: []byte{byte(i), 0}}
	}
	deadline := time.After(time.Second)
	for s.Stats().USBReads < 2 {
		select {
		case <-deadline:
			t.Fatal("producer did not reach full queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	stopped := make(chan error, 1)
	go func() { stopped <- d.Stop() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop deadlocked with a full queue")
	}
	b.reader = &fakeReader{input: make(chan readResult, 1)}
	second, err := d.Start(context.Background(), StreamOptions{Raw: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	} // closing an old stream must leave the new one running
	b.reader.input <- readResult{data: []byte{9, 0}}
	if next(t, second).Raw[0] != 9 {
		t.Fatal("wrong raw data")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRawBytesOwnMemoryAndReaderErrorPropagates(t *testing.T) {
	d, b := testDevice()
	defer d.Close()
	s, err := d.Start(context.Background(), StreamOptions{Raw: true})
	if err != nil {
		t.Fatal(err)
	}
	b.reader.input <- readResult{data: []byte{1, 2, 3}}
	p := next(t, s)
	sentinel := errors.New("disconnected")
	b.reader.input <- readResult{data: []byte{4, 5}, err: sentinel}
	next(t, s)
	_, err = s.Next(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Raw, []byte{1, 2, 3}) {
		t.Fatal("raw packet mutated")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTruncatedDecodedStreamAndFailedStartCleanup(t *testing.T) {
	d, b := testDevice()
	defer d.Close()
	b.failStart = true
	if _, err := d.Start(context.Background(), StreamOptions{}); err == nil {
		t.Fatal("accepted start failure")
	}
	if b.reader.closed != 1 {
		t.Fatal("reader leaked on failed start")
	}
	b.failStart = false
	b.reader = &fakeReader{input: make(chan readResult, 1)}
	s, err := d.Start(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b.reader.input <- readResult{data: []byte{1}, err: io.EOF}
	next(t, s)
	_, err = s.Next(context.Background())
	if !errors.Is(err, evt3.ErrTruncatedWord) {
		t.Fatal(err)
	}
}

func TestContextAndConcurrentLifecycle(t *testing.T) {
	d, _ := testDevice()
	ctx, cancel := context.WithCancel(context.Background())
	s, err := d.Start(ctx, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ConfigureBiases(ctx, BiasOffsets{}); !errors.Is(err, ErrStreaming) {
		t.Fatal(err)
	}
	short, stop := context.WithCancel(context.Background())
	stop()
	if _, err := s.Next(short); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop reader")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = d.Close(); _ = s.Stats(); _ = s.Err(); _ = d.Info() }()
	}
	wg.Wait()
	if _, err := d.Start(context.Background(), StreamOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestBiasesRestoreAndIndependentSnapshots(t *testing.T) {
	d, b := testDevice()
	if err := d.ConfigureBiases(context.Background(), BiasOffsets{DiffOn: 5}); err != nil {
		t.Fatal(err)
	}
	c := d.Biases()
	if c.Biases["diff_on"].ReadbackAbsolute != 105 {
		t.Fatal(c)
	}
	delete(c.Biases, "diff_on")
	if len(d.Biases().Biases) != 5 {
		t.Fatal("snapshot shared internal map")
	}
	if err := d.ConfigureBiases(context.Background(), BiasOffsets{DiffOn: -86}); err == nil {
		t.Fatal("invalid offset accepted")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if b.registers[0x1010]&255 != 100 {
		t.Fatal("baseline was not restored")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if b.closed != 1 {
		t.Fatal("closed backend twice")
	}
}

func TestStreamMemoryAndBufferValidation(t *testing.T) {
	for _, o := range []StreamOptions{{BufferSize: 1025}, {Transfers: -1}, {QueueDepth: -1}, {BufferSize: 4 << 20, QueueDepth: 256}} {
		if _, err := o.normalized(); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	if _, err := (StreamOptions{}).normalized(); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledNextDoesNotCancelAcquisition(t *testing.T) {
	d, b := testDevice()
	defer d.Close()
	s, err := d.Start(context.Background(), StreamOptions{Raw: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	b.reader.input <- readResult{data: []byte{7, 8}}
	if got := next(t, s).Raw; !reflect.DeepEqual(got, []byte{7, 8}) {
		t.Fatal(got)
	}
	if s.Err() != nil {
		t.Fatalf("Next cancellation terminated acquisition: %v", s.Err())
	}
}

func TestInfoSnapshotOwnsAllMutableFields(t *testing.T) {
	d, _ := testDevice()
	defer d.Close()
	d.info = Info{
		Endpoints:              []EndpointInfo{{Address: 0x81}},
		SensorIdentifiers:      []string{"psee,ccam5_imx636"},
		ReleaseVersionResponse: []byte{1}, BuildDateResponse: []byte{2},
		RegisterReads: map[uint32]uint32{0x1000: 3},
	}
	snapshot := d.Info()
	snapshot.Endpoints[0].Address = 0
	snapshot.SensorIdentifiers[0] = "changed"
	snapshot.ReleaseVersionResponse[0] = 0
	snapshot.BuildDateResponse[0] = 0
	delete(snapshot.RegisterReads, 0x1000)
	if got := d.Info(); !reflect.DeepEqual(got, d.info) {
		t.Fatalf("snapshot changed: %+v", got)
	}
	if d.info.Endpoints[0].Address != 0x81 || d.info.SensorIdentifiers[0] != "psee,ccam5_imx636" || d.info.ReleaseVersionResponse[0] != 1 || d.info.BuildDateResponse[0] != 2 || d.info.RegisterReads[0x1000] != 3 {
		t.Fatal("caller mutated device identity through snapshot")
	}
}

type closeFailureBackend struct {
	*fakeBackend
	cause error
}

func (b *closeFailureBackend) Close() error { b.fakeBackend.Close(); return b.cause }

func TestCloseErrorIsStableAndResourcesReleasedOnce(t *testing.T) {
	d, b := testDevice()
	cause := errors.New("release interface failed")
	d.transport = &closeFailureBackend{b, cause}
	for i := 0; i < 2; i++ {
		if err := d.Close(); !errors.Is(err, cause) {
			t.Fatalf("Close lost failure: %v", err)
		}
	}
	if b.closed != 1 {
		t.Fatalf("backend closed %d times", b.closed)
	}
	if err := d.ConfigureBiases(context.Background(), BiasOffsets{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.Temperature(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.Illuminance(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

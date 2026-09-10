// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package imx636

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/crosscue/go-imx636/internal/hw"
)

// Width is the supported sensor width in pixels.
const Width = 1280

// Height is the supported sensor height in pixels.
const Height = 720

// InitializationReference identifies the pinned upstream initialization source.
const InitializationReference = hw.InitializationID

// Info contains USB topology and raw camera identity responses; it may identify
// a physical device. Firmware responses are opaque, not validated versions.
type Info = hw.Info

// EndpointInfo describes a USB endpoint used by the camera.
type EndpointInfo = hw.EndpointInfo

// BiasOffsets selects signed offsets from the trim saved at Open.
type BiasOffsets = hw.BiasOffsets

// BiasValue reports requested and verified IDAC values. FactoryAbsolute means
// the value saved at Open, which need not be the nonvolatile factory trim.
type BiasValue = hw.BiasValue

// BiasConfiguration is a snapshot of the five exposed bias settings.
type BiasConfiguration = hw.BiasConfiguration

var (
	// ErrUSBUnavailable indicates that this build has no live USB backend.
	ErrUSBUnavailable = hw.ErrUSBUnavailable
	// ErrClosed indicates an operation on a closed Device.
	ErrClosed = errors.New("imx636: camera is closed")
	// ErrStreaming requires stopping the current session before this operation.
	ErrStreaming = errors.New("imx636: stop the active stream first")
	// ErrNotReady indicates an invalid sensor monitor sample.
	ErrNotReady = errors.New("imx636: sensor measurement is not ready")
)

// Options selects an IDS camera by its protocol serial (or USB descriptor serial). An empty Serial
// is allowed only when exactly one camera matches. Biases are signed offsets
// from the values saved before initialization, normally the factory trim.
type Options struct {
	Serial string
	Biases BiasOffsets
	Logger *slog.Logger
}

type backend interface {
	hw.RegisterWriter
	Drain(context.Context) error
	Reader(int, int) (hw.Reader, error)
	Close() error
}

type openingBackend interface {
	backend
	Info(context.Context) (Info, error)
	Initialize(context.Context, hw.BiasRequest) (hw.InitializationResult, error)
}

// Device owns a claimed USB interface. Obtain one with Open; the zero value
// is not usable. Methods may be called concurrently, but do not copy a Device.
// Close must be called even when Start or a stream read fails.
type Device struct {
	mu        sync.Mutex
	transport backend
	info      Info
	baseline  map[uint32]uint8
	biases    BiasConfiguration
	active    *Stream
	closed    bool
	closeErr  error
}

// List reads USB descriptors without initializing the sensor, briefly claiming the interface to read the protocol serial. A partial result
// can accompany an error when one of several cameras could not be opened.
func List(ctx context.Context) ([]Info, error) { return hw.List(ctx) }

// Open saves the existing bias trim, initializes the sensor, and leaves it
// stopped. The context applies to opening only; use Start's context for streaming.
// Synchronous libusb setup and descriptor operations may delay cancellation.
func Open(ctx context.Context, options Options) (*Device, error) {
	if err := hw.ValidateBiasOffsets(options.Biases); err != nil {
		return nil, err
	}
	handle, err := hw.Open(ctx, hw.DefaultProduct, options.Serial, options.Logger)
	if err != nil {
		return nil, err
	}
	return initializeOpenedDevice(ctx, handle, options)
}

func initializeOpenedDevice(ctx context.Context, handle openingBackend, options Options) (*Device, error) {
	info, err := handle.Info(ctx)
	if err != nil {
		return nil, errors.Join(err, handle.Close())
	}
	if !slices.Contains(info.SensorIdentifiers, "psee,ccam5_imx636") {
		return nil, errors.Join(errors.New("camera did not report an IMX636 sensor"), handle.Close())
	}
	initialized, err := handle.Initialize(ctx, hw.BiasRequest{Profile: "default", Offsets: options.Biases})
	if err != nil {
		// Initialize owns rollback once register writes begin. In particular,
		// failed baseline reads and validation must not trigger register writes.
		return nil, errors.Join(err, handle.Close())
	}
	return &Device{transport: handle, info: info, baseline: initialized.BiasIDAC, biases: initialized.BiasConfiguration}, nil
}

// Info returns an independent snapshot of the camera identity.
func (d *Device) Info() Info {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.info
	i.Endpoints = append([]EndpointInfo(nil), i.Endpoints...)
	i.SensorIdentifiers = append([]string(nil), i.SensorIdentifiers...)
	i.ReleaseVersionResponse = append([]byte(nil), i.ReleaseVersionResponse...)
	i.BuildDateResponse = append([]byte(nil), i.BuildDateResponse...)
	i.RegisterReads = make(map[uint32]uint32, len(d.info.RegisterReads))
	for k, v := range d.info.RegisterReads {
		i.RegisterReads[k] = v
	}
	return i
}

func cloneBiases(c BiasConfiguration) BiasConfiguration {
	m := make(map[string]BiasValue, len(c.Biases))
	for k, v := range c.Biases {
		m[k] = v
	}
	c.Biases = m
	return c
}

// Biases returns the last verified bias configuration.
func (d *Device) Biases() BiasConfiguration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return cloneBiases(d.biases)
}

// BiasRange returns permitted offsets for diff_on, diff_off, fo, hpf or refr.
// An unknown name returns ok=false.
func BiasRange(name string) (minimum, maximum int, ok bool) { return hw.BiasRange(name) }

// ValidateBiases checks nominal offset ranges without accessing hardware.
// Open and ConfigureBiases also check offsets against the camera baseline.
func ValidateBiases(offsets BiasOffsets) error { return hw.ValidateBiasOffsets(offsets) }

// ConfigureBiases applies and verifies offsets while stopped. A failed apply
// restores the baseline; a failed restoration is included in the returned error.
func (d *Device) ConfigureBiases(ctx context.Context, offsets BiasOffsets) error {
	if err := ValidateBiases(offsets); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	if d.active != nil {
		return ErrStreaming
	}
	c, err := hw.ApplyBiases(ctx, d.transport, d.baseline, hw.BiasRequest{Profile: "custom", Offsets: offsets})
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		restored, restoreErr := hw.ApplyBiases(cleanup, d.transport, d.baseline, hw.BiasRequest{Profile: "default"})
		d.biases = restored
		return errors.Join(err, restoreErr)
	}
	d.biases = c
	return nil
}

// Stop cancels the current stream and waits for USB transfers and sensor
// shutdown. Cleanup has its own five-second context, even if Start was canceled.
// Stop is idempotent. Stream errors remain available from Stream.Err.
func (d *Device) Stop() error { d.mu.Lock(); defer d.mu.Unlock(); return d.stopLocked() }
func (d *Device) stopLocked() error {
	if d.active == nil {
		return nil
	}
	s := d.active
	s.cancel()
	<-s.done
	d.active = nil
	return s.cleanupErr
}

// Close stops acquisition, restores the bias values saved at Open with
// readback verification, powers down the sensor, and releases USB resources.
// Repeated calls return the first Close result.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return d.closeErr
	}
	stopErr := d.stopLocked()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d.closeErr = errors.Join(stopErr, hw.StopSensor(ctx, d.transport), hw.RestoreBiases(ctx, d.transport, d.baseline), hw.DestroySensor(ctx, d.transport), d.transport.Close())
	d.closed = true
	return d.closeErr
}

// Temperature returns the sensor temperature in degrees Celsius.
func (d *Device) Temperature(ctx context.Context) (float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, ErrClosed
	}
	if err := d.transport.WriteRegister(ctx, 0x004c, 7|0xec8<<3); err != nil {
		return 0, err
	}
	v, err := d.transport.ReadRegister(ctx, 0x0050)
	if err != nil {
		return 0, err
	}
	if v&(1<<11) == 0 {
		return 0, ErrNotReady
	}
	return float64(v&0x3ff)*0.19 - 56, nil
}

// Illuminance returns the sensor's raw LIFO light measurement, not calibrated
// lux. It follows the reference driver's lifo_ton measurement.
func (d *Device) Illuminance(ctx context.Context) (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, ErrClosed
	}
	v, err := d.transport.ReadRegister(ctx, 0x0010)
	if err != nil {
		return 0, err
	}
	if v&(1<<29) == 0 {
		return 0, fmt.Errorf("illuminance: %w", ErrNotReady)
	}
	return v & 0x1fffffff, nil
}

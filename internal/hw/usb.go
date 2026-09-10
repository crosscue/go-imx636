//go:build linux && cgo && libusb

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/gousb"
)

var supportedIDs = map[[2]gousb.ID]struct{}{
	{0x1409, 0x8e00}: {},
}

type topology struct {
	configuration, interfaceNumber, alternate int
	commandOut, responseIn, eventIn           gousb.EndpointDesc
}

type endpointTransport struct {
	in  *gousb.InEndpoint
	out *gousb.OutEndpoint
}

func (t endpointTransport) WriteContext(ctx context.Context, data []byte) (int, error) {
	return t.out.WriteContext(ctx, data)
}

func (t endpointTransport) ReadContext(ctx context.Context, data []byte) (int, error) {
	return t.in.ReadContext(ctx, data)
}

type Handle struct {
	usb      *gousb.Context
	devices  []*gousb.Device
	chosen   *gousb.Device
	config   *gousb.Config
	iface    *gousb.Interface
	topology topology
	command  *Client
	eventIn  *gousb.InEndpoint
	info     Info
}

func (d *Handle) releaseInterface() error {
	if d.iface != nil {
		d.iface.Close()
		d.iface = nil
	}
	var err error
	if d.config != nil {
		err = d.config.Close()
		d.config = nil
	}
	return err
}
func (d *Handle) Close() error {
	result := d.releaseInterface()
	for _, device := range d.devices {
		result = errors.Join(result, device.Close())
	}
	d.devices = nil
	if d.usb != nil {
		d.usb.Close()
		d.usb = nil
	}
	return result
}

func (d *Handle) drainStaleEvents(ctx context.Context) error {
	buffer := make([]byte, 128<<10)
	for {
		drainCtx, cancel := context.WithTimeout(ctx, time.Second)
		n, err := d.eventIn.ReadContext(drainCtx, buffer)
		timedOut := drainCtx.Err() != nil
		cancel()
		if err != nil {
			if timedOut {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return nil
			}
			return err
		}
		if n == 0 {
			return nil
		}
	}
}

func Open(ctx context.Context, product, serial string, log *slog.Logger) (_ *Handle, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if product == "" {
		product = DefaultProduct
	}
	if log == nil {
		log = slog.Default()
	}
	usb, err := newXCPUSBContext()
	if err != nil {
		return nil, err
	}
	devices, enumErr := usb.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		_, supported := supportedIDs[[2]gousb.ID{desc.Vendor, desc.Product}]
		_, topologyOK := selectTopology(desc)
		return supported && topologyOK
	})
	if len(devices) == 0 {
		usb.Close()
		if enumErr != nil {
			return nil, fmt.Errorf("enumerate/open XCP-E devices: %w", enumErr)
		}
		return nil, fmt.Errorf("no matching XCP-E device found; expected USB product %q", product)
	}
	opened := &Handle{usb: usb, devices: devices}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, opened.Close())
		}
	}()
	if serial == "" && enumErr != nil {
		return nil, fmt.Errorf("incomplete camera enumeration: %w", enumErr)
	}
	var selectionErr error
	var candidates []*gousb.Device
	for _, candidate := range devices {
		name, nameErr := candidate.Product()
		if nameErr == nil && strings.EqualFold(strings.TrimSpace(name), product) {
			candidates = append(candidates, candidate)
		}
	}
	if serial == "" && len(candidates) > 1 {
		return nil, errors.New("multiple IDS XCP-E cameras match; specify a protocol serial number")
	}
	for _, candidate := range candidates {
		opened.chosen = candidate
		if err := claim(opened, log); err != nil {
			selectionErr = errors.Join(selectionErr, err, opened.releaseInterface())
			continue
		}
		info, err := inspectIdentity(ctx, opened)
		if err != nil {
			selectionErr = errors.Join(selectionErr, err, opened.releaseInterface())
			continue
		}
		if matchesSerial(info, serial) {
			opened.info = info
			return opened, nil
		}
		if err := opened.releaseInterface(); err != nil {
			return nil, err
		}
	}
	return nil, errors.Join(fmt.Errorf("no IDS XCP-E camera matches product %q and serial %q", product, serial), selectionErr, enumErr)
}

func claim(opened *Handle, log *slog.Logger) error {
	if err := opened.chosen.SetAutoDetach(true); err != nil {
		return fmt.Errorf("enable kernel-driver auto-detach: %w", err)
	}
	active, err := opened.chosen.ActiveConfigNum()
	if err != nil {
		return fmt.Errorf("read active XCP-E configuration: %w", err)
	}
	topology, ok := selectTopologyInConfig(opened.chosen.Desc, active)
	if !ok {
		return fmt.Errorf("XCP-E descriptor topology is not present in active configuration %d", active)
	}
	opened.topology = topology
	config, err := opened.chosen.Config(active)
	if err != nil {
		return fmt.Errorf("open XCP-E configuration %d: %w", active, err)
	}
	opened.config = config
	iface, err := config.Interface(topology.interfaceNumber, topology.alternate)
	if err != nil {
		return fmt.Errorf("claim XCP-E interface %d alt %d: %w", topology.interfaceNumber, topology.alternate, err)
	}
	opened.iface = iface
	commandOut, err := iface.OutEndpoint(topology.commandOut.Number)
	if err != nil {
		return fmt.Errorf("open XCP-E command OUT endpoint: %w", err)
	}
	responseIn, err := iface.InEndpoint(topology.responseIn.Number)
	if err != nil {
		return fmt.Errorf("open XCP-E response IN endpoint: %w", err)
	}
	eventIn, err := iface.InEndpoint(topology.eventIn.Number)
	if err != nil {
		return fmt.Errorf("open XCP-E event IN endpoint: %w", err)
	}
	opened.command = NewClient(endpointTransport{in: responseIn, out: commandOut}, log)
	opened.eventIn = eventIn
	opened.info = descriptorInfo(opened.chosen, topology)
	return nil
}

func newXCPUSBContext() (usb *gousb.Context, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			usb = nil
			err = fmt.Errorf("initialize libusb for XCP-E: %v (under WSL, verify USB/IP support and device attachment)", recovered)
		}
	}()
	return gousb.NewContext(), nil
}

func inspectIdentity(ctx context.Context, device *Handle) (Info, error) {
	info := device.info
	if err := ctx.Err(); err != nil {
		return info, err
	}
	// gousb's zero ControlTimeout means an unlimited libusb control transfer.
	// Control has no context API; bound this request even for a stalled device.
	device.chosen.ControlTimeout = defaultCommandTimeout
	board := make([]byte, 2)
	n, err := device.chosen.Control(0xc0, 0x72, 0, 0, board)
	if err != nil {
		return info, fmt.Errorf("read XCP-E board identity control request: %w", err)
	}
	if n != len(board) {
		return info, fmt.Errorf("short XCP-E board identity: got %d of %d bytes", n, len(board))
	}
	info.BoardType, info.BoardRevision = board[0], board[1]
	if info.BoardType != 0x31 && info.BoardType != 0x35 {
		return info, fmt.Errorf("unsupported XCP-E board type 0x%02x", info.BoardType)
	}
	var requestErr error
	if info.ReleaseVersionResponse, requestErr = device.command.Request(ctx, "release-version", []byte{0x79, 0, 0, 0, 0, 0, 0, 0}); requestErr != nil {
		return info, requestErr
	}
	if info.BuildDateResponse, requestErr = device.command.Request(ctx, "build-date", []byte{0x7a, 0, 0, 0, 0, 0, 0, 0}); requestErr != nil {
		return info, requestErr
	}
	serialResponse, err := device.command.Request(ctx, "protocol-serial", []byte{0x72, 0, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return info, err
	}
	if len(serialResponse) < 12 {
		return info, errors.New("short protocol serial response")
	}
	if len(serialResponse) >= 12 {
		info.ProtocolSerial = fmt.Sprintf("%02X%02X%02X%02X", serialResponse[11], serialResponse[10], serialResponse[9], serialResponse[8])
	}
	sensorResponse, err := device.command.Request(ctx, "sensor-identity", []byte{0x03, 0, 1, 0, 4, 0, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return info, err
	}
	info.SensorIdentity = hex.EncodeToString(sensorResponse)
	if len(sensorResponse) > 12 {
		for _, identifier := range strings.Split(string(sensorResponse[12:]), "\x00") {
			if identifier = strings.TrimSpace(identifier); identifier != "" {
				info.SensorIdentifiers = append(info.SensorIdentifiers, identifier)
			}
		}
	}
	info.RegisterReads = make(map[uint32]uint32)
	for _, address := range []uint32{0x1000, 0x1004, 0x9008, 0xb000} {
		value, err := device.command.ReadRegister(ctx, address)
		if err != nil {
			return info, fmt.Errorf("read identity register 0x%08x: %w", address, err)
		}
		info.RegisterReads[address] = value
	}
	return info, nil
}

func descriptorInfo(device *gousb.Device, topology topology) Info {
	manufacturer, _ := device.Manufacturer()
	product, _ := device.Product()
	serial, _ := device.SerialNumber()
	return Info{
		Bus: device.Desc.Bus, Address: device.Desc.Address, VendorID: uint16(device.Desc.Vendor), ProductID: uint16(device.Desc.Product),
		Manufacturer: manufacturer, Product: product, USBSerial: serial, USBSpeed: device.Desc.Speed.String(),
		Configuration: topology.configuration, Interface: topology.interfaceNumber, Alternate: topology.alternate,
		Endpoints: []EndpointInfo{
			{Address: uint8(topology.commandOut.Address), Direction: "out", TransferType: topology.commandOut.TransferType.String(), MaxPacketSize: topology.commandOut.MaxPacketSize},
			{Address: uint8(topology.eventIn.Address), Direction: "in", TransferType: topology.eventIn.TransferType.String(), MaxPacketSize: topology.eventIn.MaxPacketSize},
			{Address: uint8(topology.responseIn.Address), Direction: "in", TransferType: topology.responseIn.TransferType.String(), MaxPacketSize: topology.responseIn.MaxPacketSize},
		},
		ExpectedEndpointsMatch:  topology.commandOut.Address == commandEndpointAddress && topology.responseIn.Address == responseEndpointAddress && topology.eventIn.Address == eventEndpointAddress,
		InitializationReference: InitializationID,
	}
}

func selectTopology(desc *gousb.DeviceDesc) (topology, bool) {
	configs := make([]int, 0, len(desc.Configs))
	for number := range desc.Configs {
		configs = append(configs, number)
	}
	sort.Ints(configs)
	for _, number := range configs {
		if selected, ok := selectTopologyInConfig(desc, number); ok {
			return selected, true
		}
	}
	return topology{}, false
}

func selectTopologyInConfig(desc *gousb.DeviceDesc, configNumber int) (topology, bool) {
	config, ok := desc.Configs[configNumber]
	if !ok {
		return topology{}, false
	}
	for _, iface := range config.Interfaces {
		for _, alt := range iface.AltSettings {
			if uint8(alt.Class) != 0xff || uint8(alt.SubClass) != 0x19 {
				continue
			}
			selected := topology{configuration: configNumber, interfaceNumber: iface.Number, alternate: alt.Alternate}
			for _, endpoint := range alt.Endpoints {
				switch endpoint.Address {
				case commandEndpointAddress:
					selected.commandOut = endpoint
				case responseEndpointAddress:
					selected.responseIn = endpoint
				case eventEndpointAddress:
					selected.eventIn = endpoint
				}
			}
			if selected.commandOut.Address == commandEndpointAddress && selected.responseIn.Address == responseEndpointAddress && selected.eventIn.Address == eventEndpointAddress && selected.commandOut.TransferType == gousb.TransferTypeBulk && selected.responseIn.TransferType == gousb.TransferTypeBulk && selected.eventIn.TransferType == gousb.TransferTypeBulk {
				return selected, true
			}
		}
	}
	return topology{}, false
}

// List briefly claims each camera to read its protocol serial without initializing it.
func List(ctx context.Context) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	usb, err := newXCPUSBContext()
	if err != nil {
		return nil, err
	}
	defer usb.Close()
	devices, enumErr := usb.OpenDevices(func(d *gousb.DeviceDesc) bool { return d.Vendor == 0x1409 && d.Product == 0x8e00 })
	var result []Info
	for _, d := range devices {
		if ctx.Err() != nil {
			enumErr = errors.Join(enumErr, ctx.Err(), d.Close())
			continue
		}
		topology, ok := selectTopology(d.Desc)
		if !ok {
			enumErr = errors.Join(enumErr, d.Close())
			continue
		}
		h := &Handle{chosen: d, devices: []*gousb.Device{d}, info: descriptorInfo(d, topology)}
		err := claim(h, nil)
		if err == nil {
			h.info, err = inspectIdentity(ctx, h)
		}
		result = append(result, h.info)
		enumErr = errors.Join(enumErr, err, h.Close())
	}
	return result, enumErr
}
func (d *Handle) Info(ctx context.Context) (Info, error) { return d.info, ctx.Err() }
func (d *Handle) ReadRegister(ctx context.Context, a uint32) (uint32, error) {
	return d.command.ReadRegister(ctx, a)
}
func (d *Handle) WriteRegister(ctx context.Context, a, v uint32) error {
	return d.command.WriteRegister(ctx, a, v)
}
func (d *Handle) Initialize(ctx context.Context, r BiasRequest) (InitializationResult, error) {
	return initializeSensor(ctx, d.command, d.Drain, r)
}
func (d *Handle) Drain(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return d.drainStaleEvents(bounded)
}

type usbReader struct{ stream *gousb.ReadStream }

func (r *usbReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	n, err := r.stream.ReadContext(ctx, p)
	// gousb's failed transfer count is not copied into p; never expose it as valid data.
	if err != nil {
		return 0, err
	}
	return n, nil
}
func (r *usbReader) Close() error {
	err := r.stream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	buffer := make([]byte, 128<<10)
	for {
		_, readErr := r.stream.ReadContext(ctx, buffer)
		if readErr != nil {
			if !errors.Is(readErr, context.Canceled) && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrClosedPipe) && !errors.Is(readErr, gousb.TransferCancelled) {
				err = errors.Join(err, readErr)
			}
			break
		}
	}
	return err
}
func (d *Handle) Reader(size, transfers int) (Reader, error) {
	s, err := d.eventIn.NewStream(size, transfers)
	if err != nil {
		return nil, err
	}
	return &usbReader{s}, nil
}

# go-imx636

go-imx636 is an independent Go library for working with IMX636-based
event-vision hardware, initially developed by Crosscue.

**Status: experimental API.** The initial release is v0.1.0. The module path is
`github.com/crosscue/go-imx636`. The root Go package is named `imx636`;
portable decoding lives in `evt3`.

The implementation targets **IDS uEye XCP-E / UE-39B0XCP**, USB `1409:8e00`,
with a Sony IMX636 1280 × 720 event sensor. The library has been validated on
one such camera on Linux. Other models and firmware versions are unverified.

The library provides discovery, protocol-serial selection, initialization,
raw EVT3 streaming, polarity and trigger decoding, five bias controls,
temperature, and a raw light measurement. Live access uses gousb and system
libusb, with no runtime requirement for IDS peak or Metavision. This is an
unofficial project, with no claimed vendor endorsement or certification.

## Requirements

- Go 1.23 or newer.
- Live camera access: Linux, a C compiler, pkg-config, and libusb-1.0 headers.
- Permission to access the camera's USB device.
- A USB 3 connection for useful streaming throughput.

On Debian/Ubuntu:

```sh
sudo apt-get install build-essential pkg-config libusb-1.0-0-dev
```

Live programs require `CGO_ENABLED=1` and `-tags libusb`. Without that build tag,
the module still builds, offline decoding works, and hardware calls return
`imx636.ErrUSBUnavailable`. Other operating systems currently support offline
decoding only.

Applications can add the module with:

```sh
go get github.com/crosscue/go-imx636
```

For a local checkout, run the examples below from its root.

## Quick start

```sh
go run -tags libusb ./examples/list
go run -tags libusb ./examples/info
go run -tags libusb ./examples/events -duration 5s
go run -tags libusb ./examples/view
```

`list` briefly claims the interface to read the camera's protocol serial. It
does not initialize the sensor or write registers. Some XCP-E cameras have an
empty USB descriptor serial; use `ProtocolSerial` from the listing:

```sh
go run -tags libusb ./examples/events -serial YOUR_CAMERA_SERIAL -duration 5s
```

An unspecified serial is accepted only when exactly one matching camera is
available. Discovery can return partial information together with an error for
an inaccessible or busy camera. Close other camera software before opening it.

## Using the API

```go
func acquire(ctx context.Context) (result error) {
    camera, err := imx636.Open(ctx, imx636.Options{})
    if err != nil {
        return err
    }
    defer func() { result = errors.Join(result, camera.Close()) }()

    stream, err := camera.Start(ctx, imx636.StreamOptions{})
    if err != nil {
        return err
    }
    for {
        packet, err := stream.Next(ctx)
        if err != nil {
            return err
        }
        for _, event := range packet.Events {
            // event.X, event.Y: sensor coordinates
            // event.Polarity: true for ON, false for OFF
            // event.CameraTime: sensor microseconds
            _ = event
        }
        // packet.Triggers contains external-trigger edges separately.
    }
}
```

Imports for this function are `context`, `errors`, and
`github.com/crosscue/go-imx636`. See the runnable [events example](examples/events/main.go)
for signal handling, a capture-duration timer, cleanup, and throughput reporting.

`Open` leaves event output stopped. Its context applies to initialization only.
Some synchronous libusb setup/descriptor operations cannot be interrupted by
the context. The board-identity control request has a one-second timeout.
`Start`'s context controls acquisition for that session. Canceling a context
passed only to `Next` cancels that call without stopping the camera.

`Stop`, `Stream.Close`, and `Device.Close` unblock USB reads and a producer
waiting on a full packet queue. Cleanup uses an independent five-second context
for register operations. `Stop` is required before another `Start`, including
after a stream error. `Close` restores the saved bias trim, verifies readback,
powers down the sensor, and releases the USB interface. Always check cleanup
errors. Calls that stop a stream return cleanup errors; acquisition errors
remain available through `Next` and `Stream.Err`.

Device methods are synchronized. A stream has one consumer: do not call `Next`
concurrently. `Stats`, `Err`, and `Close` can run concurrently with `Next`.
Returned packets own their slices, which remain valid after later reads or
camera closure. No explicit buffer release is required.

## Examples

| Folder | Purpose | Example command |
|---|---|---|
| `list` | Discover cameras and their serials | `go run -tags libusb ./examples/list` |
| `info` | Initialize, inspect identity, biases, temperature and light | `go run -tags libusb ./examples/info` |
| `events` | Consume decoded events and report statistics | `go run -tags libusb ./examples/events -duration 5s` |
| `events` | Measure raw USB throughput | `go run -tags libusb ./examples/events -raw -duration 5s` |
| `view` | Show live camera activity in a browser | `go run -tags libusb ./examples/view` |
| `record` | Save raw EVT3 to a new file | `go run -tags libusb ./examples/record -out capture.evt3 -duration 5s` |
| `decode` | Decode a recording without a camera | `go run ./examples/decode -in capture.evt3` |
| `decode` | Run with a built-in synthetic sample | `go run ./examples/decode` |
| `biases` | Apply offsets and verify readback | `go run -tags libusb ./examples/biases -diff-on 5 -diff-off 5` |
| `sweep` | Compare ON-threshold offsets 0, 5 and 10 | `go run -tags libusb ./examples/sweep -duration 1s` |

Hardware examples accept `-serial`. Acquisition examples stop on Ctrl+C and
restore saved bias trim on exit. The sweep applies one setting while stopped,
starts a new session for each measurement, and reports event rate with readback.
These are simple diagnostics; scene changes and sensor settling affect the
measurements. They are not a calibrated characterization procedure.

Raw recordings contain **headerless little-endian EVT3 bytes**. They do not
contain a Metavision RAW header, camera identity, or timing metadata. The record
example prints identity and capture statistics as JSON on stdout; redirect it
to a sidecar file if needed. Existing output files are never overwritten. A
failed recording can leave a partial file. Cancellation ends at the last
delivered packet and can discard transfers still in flight.

## Browser camera view

```sh
go run -tags libusb ./examples/view
```

Open the printed address, normally [http://127.0.0.1:8080](http://127.0.0.1:8080),
in a browser. The page contains only the live camera preview, fitted to the
window. Use `-serial YOUR_CAMERA_SERIAL` to select a camera, `-fps 15` to reduce
preview work, or `-listen 127.0.0.1:8081` to choose another port. Ctrl+C stops the
server and acquisition and restores the camera's saved bias trim. SIGTERM also
shuts this example down cleanly.

White pixels are ON events and grey pixels are OFF events on a black background.
Events accumulate between host preview ticks (about 33 ms at the default 30 fps);
the last polarity at each pixel wins. Pixels return to black on the next frame
without activity. This displays brightness changes, not conventional video or
reconstructed scene intensity. JPEG compression may soften individual pixels.

One camera session serves all browser tabs using MJPEG. JPEG encoding and client
writes run separately from event collection. Slow browsers skip preview frames
instead of building a queue; actual frame rate depends on CPU and event rate.
The viewer is a visual diagnostic, not a recorder or a lossless event transport.
Closing a browser tab leaves the command running. Camera errors stop the server
and are reported in the terminal. A connection check replaces a stale preview
with a disconnected message within a few seconds after the server stops.

The default listener is local to this machine. Binding another interface with
`-listen` exposes an unauthenticated camera preview on that interface. This
example adds no dependencies beyond the library and the Go standard library;
all rendering and HTTP code lives in [examples/view](examples/view).

## Biases and sensor measurements

`Options.Biases` applies offsets on opening. `ConfigureBiases` changes them while
stopped. Offsets are relative to the sensor's IDAC values saved before
initialization, normally the factory trim. They are **not absolute register
values**. `Biases()` reports the requested, applied, and read-back values.

| Field | Meaning | Permitted offset |
|---|---|---|
| `DiffOn` | ON contrast threshold | −85…140 |
| `DiffOff` | OFF contrast threshold | −35…190 |
| `FO` | Low-pass bandwidth | −35…55 |
| `HPF` | High-pass filter | 0…120 |
| `Refr` | Refractory period | −20…235 |

An offset is also rejected if adding it to that camera's trim would leave the
8-bit range. These ranges were retained from an earlier Crosscue implementation.
A failed configuration attempts to restore the saved baseline; restoration
errors are returned. `Biases()` is empty if recovery could not establish a
verified configuration.

The saved values reflect the camera's state at `Open`, which may already have
been changed by other software. The library does not write nonvolatile factory
calibration. `Temperature` returns degrees Celsius. `Illuminance` returns the
reference driver's raw `lifo_ton` measurement, **not calibrated lux**. A monitor
whose validity bit is unset returns `ErrNotReady`.

## Timing, decoding and throughput

`CameraTime` is a reconstructed sensor timestamp in microseconds, not Unix time
or UTC. Start a new timing interval after restarting the camera. `HostTime` is
the time the host receives the packet; it is not exposure time or a calibrated
mapping from the camera clock. Coordinates retain the EVT3 sensor orientation,
without the reference Python driver's optional display transformations.

The decoder preserves state across arbitrary byte boundaries, expands EVT3
vectors, separates trigger events, and reconstructs 24-bit timestamp wraps.
Small backward timestamp changes are counted and clamped to retain monotonic
event times. Reserved, unsupported, malformed, and out-of-bounds words have
separate counters. Monitoring/OTHER words are counted but not interpreted.
`Finish` detects an unpaired final byte. A deliberately canceled live stream
does not assert that every final USB transfer was consumed.

`StreamOptions` defaults to 128 KiB USB buffers, eight asynchronous USB transfers,
and an eight-packet output queue. The queue blocks a producer when full; the
library does not silently evict queued packets. Sustained slow consumers can
still overflow device/USB buffers. There is no hardware loss counter here and
no guarantee of lossless capture. `Stats` reports bytes, reads, queued packets,
queue high-water, and decoder diagnostics; raw mode leaves decoder counts zero.

Each decoded packet allocates its own event slices. Use raw mode to record high
event rates with less CPU and allocation pressure. Larger queues use more memory
and increase latency. Option validation limits the estimated library buffer
budget; packets retained by application code are outside that budget.

## USB permissions and WSL

On a Linux desktop, a narrowly scoped udev rule can grant the active desktop
user access:

```text
SUBSYSTEM=="usb", ATTR{idVendor}=="1409", ATTR{idProduct}=="8e00", TAG+="uaccess"
```

Install it as `/etc/udev/rules.d/70-imx636.rules`, reload udev rules, then unplug and
reconnect the camera. Headless systems may need a local group-based access rule
instead. Under WSL the camera must first be attached to the Linux environment
through USB/IP. Check that `lsusb` lists `1409:8e00`.

## Troubleshooting USB command failures

`XCP-E release-version request 1 write: transfer failed` means the first bulk
command failed after USB discovery. The `list` example prints any partial
identity information before reporting an error; empty protocol serial and
firmware fields in that output do not establish that the camera lacks them.

On the tested native Linux setup, unplugging and reconnecting the camera
restored successful discovery without a software change. If this occurs, close
camera applications, unplug/reconnect the camera, and retry `examples/list`.
This is a recovery step; the generic transfer error does not establish whether
the original cause was camera firmware, the USB connection or the host controller.

If it recurs, capture diagnostics while the failure is still present, before
reconnecting:

```sh
LIBUSB_DEBUG=4 go run -tags libusb ./examples/list \
  > /tmp/imx636-list.json 2> /tmp/imx636-usb.log
journalctl -k --since '5 minutes ago' --no-pager
lsusb -t
```

Record what ran immediately before the failure, including whether acquisition
was interrupted. Keep these logs private and redact device identifiers before
sharing them. If failures continue after reconnecting, try a known-good USB 3
cable and another direct host port to help isolate the connection.

## Validation

```sh
go test ./...
go test -race -tags libusb ./...
go vet -tags libusb ./...
go test ./evt3 -fuzz FuzzDecoderChunking -fuzztime 10s
```

Physical tests are explicitly opt-in and change volatile camera settings:

```sh
IMX636_HARDWARE=1 IMX636_SERIAL=YOUR_CAMERA_SERIAL go test -race -tags 'libusb integration' -run '^TestHardware$' -v .
```

They exercise discovery, decoded/raw/restarted acquisition, bias readback,
monitor reads, and verification of every saved bias after closing. Replace the
serial with your camera's serial or omit it when only one camera is connected.
See [testing and hardware evidence](docs/TESTING.md) for coverage and validation limits.

## Scope and provenance

This version targets IDS USB ID `1409:8e00` and requires an IMX636 identity.
It does not claim support for every EVK4-compatible camera or firmware version.
ROI/pixel masks, synchronization modes, rate limiting, noise filtering, and
anti-flicker controls are not exposed in this version. Trigger decoding is
implemented, but external electrical triggering has not been physically tested.

The low-level initialization and decoder were adapted from an earlier Crosscue
implementation and revised for independent use. The initialization is pinned to
[`neuromorphic-drivers` revision `1e3d47d`](https://github.com/neuromorphicsystems/neuromorphic-drivers/blob/1e3d47de6c6bd7f2ddc3fe68684eed977e3f0d81/drivers/src/devices/prophesee_evk4.rs).
The Rust/Python implementation informed protocol handling and sensor monitors.
Upstream's MIT notice is retained in [third-party notices](THIRD_PARTY_NOTICES.md).

## Licence

Copyright 2026 Crosscue Ltd

Licensed under the [Apache License, Version 2.0](LICENSE). Copyright and
attribution notices are in [NOTICE](NOTICE). Third-party material retains its
applicable notices and licence terms; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md), including the upstream MIT
notice. The Apache licence permits commercial use and proprietary applications;
it does not require users to publish their application source.

## API stability and compatibility

`Info` includes raw firmware replies and physical device identifiers. Redact
these before sharing reports. The API aliases internal hardware types; their
exported fields are public API. `BiasValue.FactoryAbsolute` means trim saved at
open, not proof of factory calibration. Packet slices preserve event order
within each kind; separating triggers loses their original interleaving with
polarity events. The decoder is not safe for concurrent method calls. It does
not preserve the EVT3 master/slave bit or recover missing timestamp epochs.

## Contributing and security

Developed and maintained by Crosscue. See [CONTRIBUTING.md](CONTRIBUTING.md) for
scope, provenance requirements and checks, and [SECURITY.md](SECURITY.md) for
private reporting guidance. Do not attach private captures or identifiable
device reports to public issues.

IDS, Sony, IMX636 and Prophesee names and trademarks belong to their respective
owners. Their use here identifies hardware and references, not affiliation.

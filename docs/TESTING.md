# Testing and supported hardware

Unit tests use synthetic event bytes, register transports and camera backends.
They run without a camera. The `libusb` build also tests synthetic USB descriptor
selection; it does not open a device unless the integration test is opted into.

```sh
go test ./...
go test -race -tags libusb ./...
go vet ./...
go vet -tags libusb ./...
CGO_ENABLED=0 go test ./...
go test ./evt3 -run '^$' -fuzz '^FuzzDecoderChunking$' -fuzztime 30s -parallel 2
go test ./internal/hw -run '^$' -fuzz '^FuzzRegisterResponses$' -fuzztime 30s -parallel 2
```

Live builds need Linux, cgo and libusb development files. CI tests Go 1.23 and
the current supported Go releases, checks formatting and module integrity,
builds all examples, runs race detection and short fuzz campaigns, and checks
portable macOS/Windows compilation. CI does not validate USB hardware. If this
checkout is inside an unrelated Go workspace, set `GOWORK=off` when running
these commands.

Coverage focuses on decoder framing, timestamps and malformed vectors; request
framing and response rejection; bias validation and rollback; initialization
failures; packet ownership; cancellation, restart and cleanup; and descriptor
selection. Viewer tests cover event rendering/reset, JPEG/MJPEG output, slow
clients, multiple connections, cancellation and camera failure using synthetic
inputs and temporary localhost listeners. Coverage percentages for untagged builds exclude native USB code.
Fuzzing chunk equivalence checks consistency across chunks; it is not an
independent implementation of the EVT3 specification.

## Hardware evidence and limits

The library has been validated on Linux amd64 with one IDS UE-39B0XCP
(`1409:8e00`) reporting an IMX636 identity, using Go 1.27 and libusb 1.0.27.
Testing covered discovery, decoded and raw capture, restart, bias readback,
monitor reads, browser preview, clean shutdown, and verification of saved IDAC
values after closing. Device identifiers and raw diagnostic logs are omitted
from the public source distribution.

No claim is made for other models or firmware revisions, sustained lossless
capture, electrical trigger inputs, physical unplug/replug recovery, or live
USB on macOS/Windows. Hardware-affecting changes should be tested on the
intended camera and reported with device identifiers removed.

The following test changes volatile camera settings and requires explicit opt-in:

```sh
IMX636_HARDWARE=1 IMX636_SERIAL=YOUR_CAMERA_SERIAL \
  go test -race -tags 'libusb integration' -run '^TestHardware$' -v .
```

Omit the serial only when exactly one matching camera is attached. Preserve the
full log privately; share a redacted result. Also exercise canceled opening and
capture, a stalled/unplugged device, recovery by reopening, and cleanup error
reporting under supervised conditions. Unit tests cannot establish native USB
cancellation deadlines or verify sensor register semantics on physical hardware.

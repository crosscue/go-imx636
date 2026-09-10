# Contributing

Keep changes focused on camera discovery, lifecycle, configuration, streaming,
and generic EVT3 decoding. Application analytics, fusion and customer services
belong in separate projects. Discuss hardware API additions before implementing
them; the pre-1.0 API may change.

Explain the problem, behaviour change and checks performed. Include a focused
regression test for a bug. State the exact source, revision and licence for any
adapted protocol code, register values, fixtures or generated material. AI output
requires the same provenance review. Do not submit vendor-confidential material,
private captures, serial numbers or code you lack permission to contribute.
Unless explicitly stated otherwise, contributions intentionally submitted for
inclusion are offered under the project's [Apache-2.0 licence](LICENSE), as
described in its section 5. Preserve third-party attribution and licence notices.

Run from a clone with Go and, for the tagged checks, libusb development files:

```sh
go mod tidy
go mod verify
go test ./...
go vet ./...
go test -race -tags libusb ./...
go vet -tags libusb ./...
go build -tags libusb ./...
test -z "$(gofmt -l .)"
git diff --exit-code -- go.mod go.sum
```

Unit tests require no camera. Physical tests require Linux, cgo, libusb and
explicit opt-in; they change volatile camera settings. See README for commands.
Report the model, firmware evidence and platform for hardware changes, with
identifiers redacted. Treat other contributors respectfully and discuss code
and evidence. See SECURITY.md for vulnerability reporting.

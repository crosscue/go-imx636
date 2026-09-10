# Changelog

## [0.1.0] - Released 2026-09-10

- Apache-2.0 project licence, with SPDX copyright headers, NOTICE, and the upstream MIT notice retained.
- IDS UE-39B0XCP discovery, lifecycle, bias controls and basic sensor diagnostics.
- Portable EVT3 polarity/trigger decoding and bounded raw or decoded streams.
- Generic discovery, capture, recording and decoding examples.
- Camera-only browser preview example with MJPEG, bounded latest-frame delivery and clean shutdown.
- Public package name `imx636`; module path `github.com/crosscue/go-imx636`.
- Prevent malformed vector-coordinate overflow and bound board-identity control reads.
- Failed opening no longer writes cleanup registers before baseline validation or repeats initialization rollback.
- Regression tests for opening failures, snapshot ownership, cancellation, cleanup errors and USB topology; protocol response fuzzing.
- Record example creates owner-readable/writable files (0600).
- Unit tests, optional hardware tests and CI checks; conservative support documentation.

There are no historical releases recorded here.

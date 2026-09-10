// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// Package imx636 controls IDS uEye XCP-E cameras with Sony IMX636 event sensors.
//
// Open initializes a camera but leaves event output stopped. Start returns a
// bounded stream of decoded events or raw EVT3 bytes. Stop cancels the reader
// and stops sensor output; Close also restores the bias values saved at Open.
// Hardware access requires Linux, cgo, libusb-1.0, and the libusb build tag.
// Package evt3 works without USB support on all Go platforms.
package imx636

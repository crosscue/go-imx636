# Third-party notices

The IMX636 initialization register sequences, protocol reference, and sensor
monitor readouts derive from or were checked against Neuromorphic Drivers:

- Project: https://github.com/neuromorphicsystems/neuromorphic-drivers
- Source: `drivers/src/devices/prophesee_evk4.rs`
- Initialization revision: `1e3d47de6c6bd7f2ddc3fe68684eed977e3f0d81`
- Copyright (c) 2020 International Centre for Neuromorphic Systems
- License: MIT; the complete notice is in
  [docs/NEUROMORPHIC-DRIVERS-LICENSE](docs/NEUROMORPHIC-DRIVERS-LICENSE).

The optional hardware backend depends on `github.com/google/gousb v1.1.3`
(Apache-2.0) and dynamically links the system libusb-1.0 library (LGPL-2.1-or-later).
Their source and license notices remain in their respective distributions.

## Project origin and licensing

The standalone go-imx636 project is licensed under Apache-2.0; this does not
replace the MIT terms or copyright notice for material derived from
Neuromorphic Drivers. The Apache [NOTICE](NOTICE) file summarises project
copyright and that attribution. The upstream notice linked above must accompany
distributions containing that material.

Native linking follows the installed libusb pkg-config settings. The normal
system build uses shared libusb; custom static builds and redistributed binaries
need a separate review of LGPL obligations. No vendor SDK, firmware image,
generated SDK binding or vendor header is bundled in this source tree.

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

const (
	DefaultProduct   = "UE-39B0XCP"
	ReferenceCommit  = "1e3d47de6c6bd7f2ddc3fe68684eed977e3f0d81"
	InitializationID = "neuromorphic-drivers/prophesee_evk4@" + ReferenceCommit
)

type EndpointInfo struct {
	Address       uint8
	Direction     string
	TransferType  string
	MaxPacketSize int
}

type Info struct {
	Bus, Address              int
	VendorID, ProductID       uint16
	Manufacturer, Product     string
	USBSerial, ProtocolSerial string
	USBSpeed                  string
	Configuration             int
	Interface, Alternate      int
	Endpoints                 []EndpointInfo
	ExpectedEndpointsMatch    bool
	BoardType, BoardRevision  byte
	SensorIdentity            string
	SensorIdentifiers         []string
	RegisterReads             map[uint32]uint32
	ReleaseVersionResponse    []byte
	BuildDateResponse         []byte
	InitializationReference   string
}

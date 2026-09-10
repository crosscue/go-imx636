// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0
//
// USB command framing in this file was adapted from or checked against
// Neuromorphic Drivers (MIT), Copyright (c) 2020 International Centre for
// Neuromorphic Systems. The complete MIT notice is in
// docs/NEUROMORPHIC-DRIVERS-LICENSE.

package hw

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	commandEndpointAddress  = 0x02
	responseEndpointAddress = 0x82
	eventEndpointAddress    = 0x81
	registerMessageBytes    = 20
	defaultCommandTimeout   = time.Second
)

var (
	ErrShortWrite       = errors.New("short XCP-E command write")
	ErrShortResponse    = errors.New("short XCP-E command response")
	ErrResponseMismatch = errors.New("XCP-E command response does not echo request")
)

type BulkTransport interface {
	WriteContext(context.Context, []byte) (int, error)
	ReadContext(context.Context, []byte) (int, error)
}

type Client struct {
	transport BulkTransport
	log       *slog.Logger
	timeout   time.Duration
	mu        sync.Mutex
	sequence  atomic.Uint64
}

func NewClient(transport BulkTransport, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{transport: transport, log: log, timeout: defaultCommandTimeout}
}

func RegisterReadRequest(address uint32) [registerMessageBytes]byte {
	var request [registerMessageBytes]byte
	copy(request[:], []byte{0x02, 0x01, 0x01, 0x00, 0x0c})
	binary.LittleEndian.PutUint32(request[12:16], address)
	binary.LittleEndian.PutUint32(request[16:20], 1)
	return request
}

func RegisterWriteRequest(address, value uint32) [registerMessageBytes]byte {
	var request [registerMessageBytes]byte
	copy(request[:], []byte{0x02, 0x01, 0x01, 0x40, 0x0c})
	binary.LittleEndian.PutUint32(request[12:16], address)
	binary.LittleEndian.PutUint32(request[16:20], value)
	return request
}

func ParseRegisterReadResponse(request [registerMessageBytes]byte, response []byte) (uint32, error) {
	if len(response) < registerMessageBytes {
		return 0, fmt.Errorf("%w: got %d bytes, need %d", ErrShortResponse, len(response), registerMessageBytes)
	}
	if !bytes.Equal(response[:16], request[:16]) {
		return 0, fmt.Errorf("%w for register 0x%08x", ErrResponseMismatch, binary.LittleEndian.Uint32(request[12:16]))
	}
	return binary.LittleEndian.Uint32(response[16:20]), nil
}

func ValidateRegisterWriteResponse(request [registerMessageBytes]byte, response []byte) error {
	const writeACKBytes = 16
	if len(response) < writeACKBytes {
		return fmt.Errorf("%w: got %d bytes, need %d", ErrShortResponse, len(response), writeACKBytes)
	}
	if !bytes.Equal(response[:4], request[:4]) || binary.LittleEndian.Uint32(response[4:8]) != 8 || !bytes.Equal(response[8:16], request[8:16]) {
		return fmt.Errorf("%w for register 0x%08x: got %x want %x", ErrResponseMismatch, binary.LittleEndian.Uint32(request[12:16]), response[:writeACKBytes], request[:writeACKBytes])
	}
	return nil
}

func (c *Client) Request(ctx context.Context, operation string, request []byte) ([]byte, error) {
	if len(request) == 0 {
		return nil, errors.New("empty XCP-E request")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestNumber := c.sequence.Add(1)
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	written, err := c.transport.WriteContext(requestCtx, request)
	if err != nil {
		return nil, fmt.Errorf("XCP-E %s request %d write: %w", operation, requestNumber, err)
	}
	if written != len(request) {
		return nil, fmt.Errorf("%w: request %d wrote %d of %d bytes", ErrShortWrite, requestNumber, written, len(request))
	}
	response := make([]byte, 1024)
	read, err := c.transport.ReadContext(requestCtx, response)
	if err != nil {
		return nil, fmt.Errorf("XCP-E %s request %d read: %w", operation, requestNumber, err)
	}
	if read < 0 || read > len(response) {
		return nil, errors.New("invalid XCP-E response byte count")
	}
	response = response[:read]
	c.log.Debug("XCP-E transaction", "request_id", requestNumber, "operation", operation, "request_length", len(request), "response_length", read)
	return response, nil
}

func (c *Client) ReadRegister(ctx context.Context, address uint32) (uint32, error) {
	request := RegisterReadRequest(address)
	response, err := c.Request(ctx, "register-read", request[:])
	if err != nil {
		return 0, err
	}
	value, err := ParseRegisterReadResponse(request, response)
	if err != nil {
		return 0, err
	}
	c.log.Debug("XCP-E register read", "register", fmt.Sprintf("0x%08x", address), "value", fmt.Sprintf("0x%08x", value), "response_length", len(response))
	return value, nil
}

func (c *Client) WriteRegister(ctx context.Context, address, value uint32) error {
	request := RegisterWriteRequest(address, value)
	response, err := c.Request(ctx, "register-write", request[:])
	if err != nil {
		return err
	}
	if err := ValidateRegisterWriteResponse(request, response); err != nil {
		return err
	}
	c.log.Debug("XCP-E register write", "register", fmt.Sprintf("0x%08x", address), "value", fmt.Sprintf("0x%08x", value), "response_length", len(response))
	return nil
}

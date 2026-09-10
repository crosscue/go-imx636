// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package hw

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

type scriptedTransport struct {
	written       []byte
	response      []byte
	writeN, readN int
	writeErr      error
	readErr       error
}

func (t *scriptedTransport) WriteContext(_ context.Context, data []byte) (int, error) {
	t.written = append([]byte(nil), data...)
	if t.writeN != 0 || t.writeErr != nil {
		return t.writeN, t.writeErr
	}
	return len(data), nil
}

func (t *scriptedTransport) ReadContext(_ context.Context, data []byte) (int, error) {
	if t.readErr != nil {
		return 0, t.readErr
	}
	n := copy(data, t.response)
	if t.readN != 0 {
		n = t.readN
	}
	return n, nil
}

func TestRegisterRequestFraming(t *testing.T) {
	read := RegisterReadRequest(0x12345678)
	if read[3] != 0 || binary.LittleEndian.Uint32(read[12:16]) != 0x12345678 || binary.LittleEndian.Uint32(read[16:20]) != 1 {
		t.Fatalf("read request %x", read)
	}
	write := RegisterWriteRequest(0x12345678, 0xaabbccdd)
	if write[3] != 0x40 || binary.LittleEndian.Uint32(write[12:16]) != 0x12345678 || binary.LittleEndian.Uint32(write[16:20]) != 0xaabbccdd {
		t.Fatalf("write request %x", write)
	}
}

func TestReadRegisterResponseParsing(t *testing.T) {
	request := RegisterReadRequest(0x1000)
	response := request
	binary.LittleEndian.PutUint32(response[16:20], 0xdeadbeef)
	value, err := ParseRegisterReadResponse(request, response[:])
	if err != nil || value != 0xdeadbeef {
		t.Fatalf("value=0x%x err=%v", value, err)
	}
	if _, err := ParseRegisterReadResponse(request, response[:19]); !errors.Is(err, ErrShortResponse) {
		t.Fatalf("short response error %v", err)
	}
	response[8]++
	if _, err := ParseRegisterReadResponse(request, response[:]); !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("mismatched response error %v", err)
	}
}

func TestWriteRegisterAcknowledgementParsing(t *testing.T) {
	request := RegisterWriteRequest(0x0004, roiStopped)
	physicalACK := []byte{0x02, 0x01, 0x01, 0x40, 0x08, 0, 0, 0, 0, 0, 0, 0, 0x04, 0, 0, 0}
	if err := ValidateRegisterWriteResponse(request, physicalACK); err != nil {
		t.Fatalf("rejected physical 16-byte ACK shape: %v", err)
	}
	if err := ValidateRegisterWriteResponse(request, physicalACK[:15]); !errors.Is(err, ErrShortResponse) {
		t.Fatalf("short ACK error %v", err)
	}
	mismatch := append([]byte(nil), physicalACK...)
	mismatch[8]++
	if err := ValidateRegisterWriteResponse(request, mismatch); !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("mismatched ACK error %v", err)
	}
	malformedLength := append([]byte(nil), physicalACK...)
	malformedLength[4] = 9
	if err := ValidateRegisterWriteResponse(request, malformedLength); !errors.Is(err, ErrResponseMismatch) {
		t.Fatalf("malformed ACK length error %v", err)
	}
}

func TestClientShortWriteAndTransportErrors(t *testing.T) {
	request := RegisterReadRequest(0)
	transport := &scriptedTransport{writeN: 19}
	client := NewClient(transport, nil)
	if _, err := client.ReadRegister(context.Background(), 0); !errors.Is(err, ErrShortWrite) {
		t.Fatalf("short write error %v", err)
	}
	transport = &scriptedTransport{writeErr: errors.New("write failed")}
	client = NewClient(transport, nil)
	if _, err := client.Request(context.Background(), "test", request[:]); err == nil {
		t.Fatal("accepted transport write failure")
	}
}

func TestClientReadRegisterRoundTrip(t *testing.T) {
	request := RegisterReadRequest(0x1040)
	response := request
	binary.LittleEndian.PutUint32(response[16:20], 0x10203040)
	transport := &scriptedTransport{response: response[:]}
	client := NewClient(transport, nil)
	value, err := client.ReadRegister(context.Background(), 0x1040)
	if err != nil || value != 0x10203040 {
		t.Fatalf("value=0x%x err=%v", value, err)
	}
	if string(transport.written) != string(request[:]) {
		t.Fatalf("written %x want %x", transport.written, request)
	}
}

func TestClientCanceledRequestDoesNotWrite(t *testing.T) {
	transport := &scriptedTransport{}
	client := NewClient(transport, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ReadRegister(ctx, 0x1000); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(transport.written) != 0 {
		t.Fatal("canceled request reached transport")
	}
}

func TestClientRejectsInvalidResponseByteCounts(t *testing.T) {
	for _, n := range []int{-1, 1025} {
		client := NewClient(&scriptedTransport{readN: n}, nil)
		if _, err := client.ReadRegister(context.Background(), 0x1000); err == nil {
			t.Fatalf("accepted byte count %d", n)
		}
	}
}

func FuzzRegisterResponses(f *testing.F) {
	read := RegisterReadRequest(0x1000)
	f.Add(uint32(0x1000), read[:])
	f.Add(uint32(0x0004), []byte{2, 1, 1, 0x40, 8, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0})
	f.Add(uint32(0), []byte{})
	f.Fuzz(func(t *testing.T, address uint32, response []byte) {
		request := RegisterReadRequest(address)
		value, err := ParseRegisterReadResponse(request, response)
		if err == nil {
			if len(response) < 20 || string(response[:16]) != string(request[:16]) || value != binary.LittleEndian.Uint32(response[16:20]) {
				t.Fatal("accepted malformed read response")
			}
		}
		write := RegisterWriteRequest(address, value)
		if err := ValidateRegisterWriteResponse(write, response); err == nil {
			if len(response) < 16 || string(response[:4]) != string(write[:4]) || binary.LittleEndian.Uint32(response[4:8]) != 8 || string(response[8:16]) != string(write[8:16]) {
				t.Fatal("accepted malformed write response")
			}
		}
	})
}

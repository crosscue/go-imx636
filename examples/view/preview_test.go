// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crosscue/go-imx636"
)

func TestAccumulationPolarityCoordinatesAndReset(t *testing.T) {
	a := newAccumulator(8, 4)
	a.add([]imx636.Event{
		{X: 3, Y: 1, Polarity: true},
		{X: 6, Y: 2, Polarity: false},
		{X: 2, Y: 0, Polarity: true},
		{X: 2, Y: 0, Polarity: false}, // Last event at the pixel wins.
		{X: 8, Y: 0, Polarity: true},  // Malformed coordinates are ignored.
		{X: 0, Y: 4, Polarity: true},
	})
	frame := image.NewGray(a.pixels.Rect)
	a.snapshot(frame)
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			want := uint8(0)
			if x == 3 && y == 1 {
				want = 255
			} else if (x == 6 && y == 2) || (x == 2 && y == 0) {
				want = 128
			}
			if got := frame.GrayAt(x, y).Y; got != want {
				t.Fatalf("pixel (%d,%d)=%d, want %d", x, y, got, want)
			}
		}
	}
	a.snapshot(frame)
	for _, p := range frame.Pix {
		if p != 0 {
			t.Fatal("inactive frame retained earlier events")
		}
	}
}

func TestRendererPublishesIdleFramesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := newLatestFrame()
	_, changed := frames.snapshot()
	done := make(chan error, 1)
	go func() { done <- renderFrames(ctx, newAccumulator(16, 8), frames, time.Millisecond) }()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("no preview without camera events")
	}
	encoded, _ := frames.snapshot()
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 16, 8) {
		t.Fatalf("invalid preview: %v", err)
	}
	copyBefore := append([]byte(nil), encoded...)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("renderer did not stop")
	}
	if !bytes.Equal(encoded, copyBefore) {
		t.Fatal("published JPEG memory was reused")
	}
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestHTTPPageAndMJPEG(t *testing.T) {
	frames := newLatestFrame()
	encoded := testJPEG(t)
	frames.publish(encoded)
	server := httptest.NewServer(previewHandler(frames))
	defer server.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/", "/health", "/missing"} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if path == "/" && (response.StatusCode != 200 || !strings.Contains(string(body), `src="/stream"`)) {
			t.Fatalf("invalid viewer page: %s", body)
		}
		if path == "/missing" && response.StatusCode != 404 {
			t.Fatal("unknown path served viewer")
		}
		if path == "/health" && (response.StatusCode != 204 || len(body) != 0) {
			t.Fatal("invalid connection check response")
		}
	}
	// Two browser connections receive the same frame independently.
	for i := 0; i < 2; i++ {
		response, err := client.Get(server.URL + "/stream")
		if err != nil {
			t.Fatal(err)
		}
		mediaType, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/x-mixed-replace" {
			t.Fatalf("invalid stream content type: %v", err)
		}
		reader := multipart.NewReader(response.Body, params["boundary"])
		// Publish once more so the multipart reader can see the next boundary.
		frames.publish(encoded)
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(part)
		response.Body.Close()
		if err != nil || !bytes.Equal(got, encoded) || part.Header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("invalid JPEG part: %v", err)
		}
	}
	response, err := client.Head(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
}

type blockedBrowser struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
}

func (w *blockedBrowser) Header() http.Header              { return w.header }
func (w *blockedBrowser) WriteHeader(int)                  {}
func (w *blockedBrowser) SetWriteDeadline(time.Time) error { return nil }
func (w *blockedBrowser) Write([]byte) (int, error) {
	close(w.started)
	<-w.release
	return 0, io.ErrClosedPipe
}

func TestSlowBrowserDoesNotBlockNewFrames(t *testing.T) {
	frames := newLatestFrame()
	frames.publish([]byte("first"))
	w := &blockedBrowser{header: make(http.Header), started: make(chan struct{}), release: make(chan struct{})}
	defer close(w.release)
	done := make(chan struct{})
	go func() {
		previewHandler(frames).ServeHTTP(w, httptest.NewRequest("GET", "/stream", nil))
		close(done)
	}()
	select {
	case <-w.started:
	case <-time.After(time.Second):
		t.Fatal("browser did not begin its write")
	}
	published := make(chan struct{})
	go func() {
		frames.publish([]byte("second"))
		frames.publish([]byte("latest"))
		close(published)
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("slow browser blocked publication")
	}
	got, _ := frames.snapshot()
	if string(got) != "latest" {
		t.Fatalf("latest frame=%q", got)
	}
	// The deferred release unblocks the handler even if a test assertion fails.
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("failed browser handler did not return")
		}
	})
}

type fakeSource struct {
	failure chan error
	entered chan struct{}
}

func (s *fakeSource) Next(ctx context.Context) (*imx636.Packet, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-s.failure:
		return nil, err
	}
}

func TestPreviewStopsOnCancellationAndCameraFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmtBool(fail), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &fakeSource{failure: make(chan error, 1), entered: make(chan struct{}, 1)}
			done := make(chan error, 1)
			go func() { done <- servePreview(ctx, listener, source, time.Millisecond) }()
			select {
			case <-source.entered:
			case <-time.After(time.Second):
				t.Fatal("acquisition did not start")
			}
			client := &http.Client{Timeout: 2 * time.Second}
			response, err := client.Get("http://" + listener.Addr().String() + "/stream")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			cause := errors.New("camera disconnected")
			if fail {
				source.failure <- cause
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if fail && !errors.Is(err, cause) || !fail && err != nil {
					t.Fatalf("shutdown error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("preview shutdown hung")
			}
			conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err == nil {
				conn.Close()
				t.Fatal("HTTP listener remained open")
			}
		})
	}
}

func fmtBool(fail bool) string {
	if fail {
		return "camera failure"
	}
	return "cancellation"
}

// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/crosscue/go-imx636"
)

const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>IMX636 camera</title>
<style>
html,body{margin:0;width:100%;height:100%;background:#000;color:#ddd}
body{display:grid;place-items:center;font:16px system-ui,sans-serif}
img{display:block;width:100%;height:100%;object-fit:contain}
[hidden]{display:none}p{padding:24px;text-align:center}
</style>
</head>
<body>
<img id="camera" src="/stream" alt="Live IMX636 event preview">
<p id="disconnected" hidden>Camera stream disconnected. Refresh to reconnect.</p>
<script>
const camera = document.getElementById('camera');
function disconnected() {
  camera.hidden = true;
  camera.removeAttribute('src');
  document.getElementById('disconnected').hidden = false;
}
camera.onerror = disconnected;
// Browsers can retain the final MJPEG image after its connection closes.
async function checkConnection() {
  if (camera.hidden) return;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 3000);
  try {
    const response = await fetch('/health', {cache: 'no-store', signal: controller.signal});
    if (!response.ok) throw new Error('disconnected');
    setTimeout(checkConnection, 2000);
  } catch (_) {
    disconnected();
  } finally {
    clearTimeout(timeout);
  }
}
setTimeout(checkConnection, 2000);
</script>
</body>
</html>
`

func previewHandler(frames *latestFrame) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, page)
	})
	mux.HandleFunc("GET /stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			return
		}
		controller := http.NewResponseController(w)
		for {
			if r.Context().Err() != nil {
				return
			}
			jpeg, changed := frames.snapshot()
			if jpeg != nil {
				// Bound each write, rather than the lifetime of this live response.
				if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
					return
				}
				if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(jpeg)); err != nil {
					return
				}
				if _, err := w.Write(jpeg); err != nil {
					return
				}
				if _, err := io.WriteString(w, "\r\n"); err != nil {
					return
				}
				if err := controller.Flush(); err != nil {
					return
				}
				if err := controller.SetWriteDeadline(time.Time{}); err != nil {
					return
				}
			}
			select {
			case <-r.Context().Done():
				return
			case <-changed:
			}
		}
	})
	return mux
}

// servePreview owns the listener, but the caller owns and closes the camera.
// HTTP clients share one acquisition session. Disconnecting a browser does not
// stop acquisition, and a camera failure stops the server instead of freezing
// the last image indefinitely.
func servePreview(parent context.Context, listener net.Listener, source packetSource, interval time.Duration) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a := newAccumulator(imx636.Width, imx636.Height)
	frames := newLatestFrame()
	server := &http.Server{
		Handler:           previewHandler(frames),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	finished := make(chan error, 3)
	go func() { finished <- collectEvents(ctx, source, a) }()
	go func() { finished <- renderFrames(ctx, a, frames, interval) }()
	go func() { finished <- server.Serve(listener) }()

	var result error
	remaining := 3
	select {
	case <-parent.Done():
	case result = <-finished:
		remaining--
		if parent.Err() != nil {
			result = nil
		}
	}
	cancel()
	closeErr := server.Close() // Also unblocks writes to disconnected/slow clients.
	for ; remaining > 0; remaining-- {
		<-finished
	}
	return errors.Join(result, closeErr)
}

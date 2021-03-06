package sockjs

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestHandler_EventSource(t *testing.T) {
	rw := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/server/session/eventsource", nil)
	h := newTestHandler()
	h.options.ResponseLimit = 1024
	go func() {
		var sess *session
		for exists := false; !exists; {
			runtime.Gosched()
			h.sessionsMux.Lock()
			sess, exists = h.sessions["session"]
			h.sessionsMux.Unlock()
		}
		for exists := false; !exists; {
			runtime.Gosched()
			sess.mux.RLock()
			exists = sess.recv != nil
			sess.mux.RUnlock()
		}
		if rt := sess.ReceiverType(); rt != ReceiverTypeEventSource {
			t.Errorf("Unexpected recevier type, got '%v', extected '%v'", rt, ReceiverTypeEventSource)
		}
		sess.mux.RLock()
		sess.recv.close()
		sess.mux.RUnlock()
	}()
	h.eventSource(rw, req)

	contentType := rw.Header().Get("content-type")
	expected := "text/event-stream; charset=UTF-8"
	if contentType != expected {
		t.Errorf("Unexpected content type, got '%s', extected '%s'", contentType, expected)
	}
	if rw.Code != http.StatusOK {
		t.Errorf("Unexpected response code, got '%d', expected '%d'", rw.Code, http.StatusOK)
	}

	if rw.Body.String() != "\r\ndata: o\r\n\r\n" {
		t.Errorf("Event stream prelude, got '%s'", rw.Body)
	}
}

func TestHandler_EventSourceMultipleConnections(t *testing.T) {
	h := newTestHandler()
	h.options.ResponseLimit = 1024
	rw := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/server/sess/eventsource", nil)
	go func() {
		rw := httptest.NewRecorder()
		h.eventSource(rw, req)
		if rw.Body.String() != "\r\ndata: c[2010,\"Another connection still open\"]\r\n\r\n" {
			t.Errorf("wrong, got '%v'", rw.Body)
		}
		h.sessionsMux.Lock()
		sess := h.sessions["sess"]
		sess.close()
		h.sessionsMux.Unlock()
	}()
	h.eventSource(rw, req)
}

func TestHandler_EventSourceConnectionInterrupted(t *testing.T) {
	h := newTestHandler()
	sess := newTestSession()
	sess.state = SessionActive
	h.sessions["session"] = sess
	req, _ := http.NewRequest("POST", "/server/session/eventsource", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rw := httptest.NewRecorder()
	cancel()
	h.eventSource(rw, req)
	select {
	case <-sess.closeCh:
	case <-time.After(1 * time.Second):
		t.Errorf("session close channel should be closed")
	}
	sess.mux.Lock()
	if sess.state != SessionClosed {
		t.Errorf("session should be closed")
	}
}

func TestEventSourceFrameWriter(t *testing.T) {
	writer := new(eventSourceFrameWriter)
	out := new(bytes.Buffer)

	// Confirm that "important" characters are escaped, but others pass
	// through unmodified.
	_, err := writer.write(out, "escaped: %\r\n;unescaped: +&#")
	if err != nil {
		t.Errorf("unexpected write error: %s", err)
	}
	if out.String() != "data: escaped: %25%0D%0A;unescaped: +&#\r\n\r\n" {
		t.Errorf("wrong, got '%v'", out.String())
	}
}

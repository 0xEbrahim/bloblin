package native

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testHandler(logger *slog.Logger, path string, handler gin.HandlerFunc) http.Handler {
	router := NewHandler(logger, func() bool { return true }).(*gin.Engine)
	router.GET(path, handler)
	return router
}

func TestHealthAndReadiness(t *testing.T) {
	ready := false
	handler := NewHandler(slog.New(slog.NewJSONHandler(io.Discard, nil)), func() bool { return ready })
	tests := []struct {
		path   string
		method string
		status int
		body   string
	}{
		{"/healthz", http.MethodGet, http.StatusOK, `{"status":"ok"}`},
		{"/readyz", http.MethodGet, http.StatusServiceUnavailable, `{"error":{"code":"not_ready","message":"server is not ready"`},
		{"/missing", http.MethodGet, http.StatusNotFound, `{"error":{"code":"not_found"`},
		{"/healthz", http.MethodPost, http.StatusMethodNotAllowed, `{"error":{"code":"method_not_allowed"`},
		{"/readyz", http.MethodPost, http.StatusMethodNotAllowed, `{"error":{"code":"method_not_allowed"`},
		{"/healthz/", http.MethodGet, http.StatusNotFound, `{"error":{"code":"not_found"`},
		{"/healthz", http.MethodHead, http.StatusMethodNotAllowed, `{"error":{"code":"method_not_allowed"`},
		{"/v1/healthz", http.MethodGet, http.StatusNotFound, `{"error":{"code":"not_found"`},
		{"/v1/readyz", http.MethodGet, http.StatusNotFound, `{"error":{"code":"not_found"`},
		{"/v1/buckets", http.MethodGet, http.StatusNotFound, `{"error":{"code":"not_found"`},
	}
	for _, tc := range tests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.status || !strings.HasPrefix(recorder.Body.String(), tc.body) {
			t.Fatalf("%s %s: got %d %s", tc.method, tc.path, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Content-Type") != "application/json" || recorder.Header().Get("X-Request-ID") == "" {
			t.Fatalf("missing JSON or request ID headers: %v", recorder.Header())
		}
		if tc.status == http.StatusMethodNotAllowed && recorder.Header().Get("Allow") != http.MethodGet {
			t.Fatal("method error omitted Allow header")
		}
		if tc.status >= 400 {
			var body struct {
				Error struct {
					RequestID string `json:"request_id"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error.RequestID != recorder.Header().Get("X-Request-ID") {
				t.Fatalf("request ID missing from error body: %s, %v", recorder.Body.String(), err)
			}
		}
	}
	ready = true
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"status":"ready"}` {
		t.Fatalf("ready response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRequestLogging(t *testing.T) {
	for _, empty := range []bool{false, true} {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		handler := testHandler(logger, "/example", func(c *gin.Context) {
			if !empty {
				c.Status(http.StatusAccepted)
				_, _ = c.Writer.Write([]byte("hello"))
			}
		})
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/example?secret=hidden", nil)
		request.Header.Set("X-Request-ID", "untrusted-client-id")
		handler.ServeHTTP(response, request)
		var entry struct {
			RequestID     string  `json:"request_id"`
			Method        string  `json:"method"`
			Path          string  `json:"path"`
			Status        int     `json:"status"`
			DurationMS    float64 `json:"duration_ms"`
			ResponseBytes int64   `json:"response_bytes"`
		}
		if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		wantStatus, wantBytes := http.StatusAccepted, int64(5)
		if empty {
			wantStatus, wantBytes = http.StatusOK, 0
		}
		if entry.RequestID != response.Header().Get("X-Request-ID") || entry.RequestID == "untrusted-client-id" || entry.Method != http.MethodGet || entry.Path != "/example" || entry.Status != wantStatus || entry.ResponseBytes != wantBytes || entry.DurationMS < 0 {
			t.Fatalf("unexpected request log: %s", logs.String())
		}
	}
}

func TestStreamingFlushBeforeHandlerCompletes(t *testing.T) {
	release := make(chan struct{})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(testHandler(logger, "/", func(c *gin.Context) {
		w := c.Writer
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write([]byte("first"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-release
		_, _ = w.Write([]byte("second"))
	}))
	defer server.Close()
	// Release the handler before server.Close even when an assertion fails.
	defer close(release)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	first := make([]byte, 5)
	if _, err := io.ReadFull(response.Body, first); err != nil || string(first) != "first" {
		t.Fatalf("first chunk was not streamed: %q, %v", first, err)
	}
}

func TestCopyCountsBytesAndPreservesResponseController(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := testHandler(logger, "/stream", func(c *gin.Context) {
		// Hide WriterTo to exercise io.Copy's bounded-buffer path through Gin.
		source := struct{ io.Reader }{strings.NewReader("streamed bytes")}
		n, err := io.Copy(c.Writer, source)
		if err != nil || n != 14 {
			t.Errorf("io.Copy: n=%d err=%v", n, err)
		}
		if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now()); !errors.Is(err, http.ErrNotSupported) {
			t.Errorf("controller did not preserve underlying capabilities: %v", err)
		}
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stream", nil))
	var entry struct {
		ResponseBytes int64 `json:"response_bytes"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || response.Body.String() != "streamed bytes" || entry.ResponseBytes != 14 {
		t.Fatalf("copy response: %d %q, logs: %s", response.Code, response.Body.String(), logs.String())
	}
}

func TestStatusCanChangeBeforeResponseIsCommitted(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(testHandler(logger, "/", func(c *gin.Context) {
		c.Status(http.StatusAccepted)
		c.Status(http.StatusCreated)
		_, _ = c.Writer.Write([]byte("created"))
	}))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("final status = %d, want 201", response.StatusCode)
	}
}

func TestPanicAfterFlushAbortsStream(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(testHandler(logger, "/", func(c *gin.Context) {
		_, _ = c.Writer.Write([]byte("partial"))
		c.Writer.Flush()
		panic("private stream failure")
	}))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err == nil || string(body) != "partial" {
		t.Fatalf("expected aborted partial stream, got %q, %v", body, err)
	}
}

func TestPanicRecoveryAndStreaming(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	panicHandler := testHandler(logger, "/panic", func(*gin.Context) {
		panic("private detail")
	})
	recorder := httptest.NewRecorder()
	panicHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "private detail") || !strings.Contains(logs.String(), "private detail") {
		t.Fatalf("panic response/log mismatch: %d %s %s", recorder.Code, recorder.Body.String(), logs.String())
	}

	streamHandler := testHandler(logger, "/stream", func(c *gin.Context) {
		_, _ = io.Copy(c.Writer, strings.NewReader("first"))
		c.Writer.Flush()
		_, _ = c.Writer.Write([]byte("second"))
	})
	recorder = httptest.NewRecorder()
	streamHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "firstsecond" || !recorder.Flushed {
		t.Fatalf("streaming response: %d %q flushed=%v", recorder.Code, recorder.Body.String(), recorder.Flushed)
	}
}

func TestRequestContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	started := make(chan struct{})
	serviceDone := make(chan error, 1)
	// Services receive a standard context, never a Gin context.
	serviceCall := func(ctx context.Context) {
		if requestID(ctx) == "" {
			serviceDone <- errors.New("request ID missing from request context")
			return
		}
		close(started)
		<-ctx.Done()
		serviceDone <- ctx.Err()
	}
	server := httptest.NewServer(testHandler(logger, "/", func(c *gin.Context) {
		serviceCall(c.Request.Context())
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Do(request)
		if response != nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("service call did not start")
	}
	cancel()
	select {
	case err := <-serviceDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("service context was not canceled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service context was not canceled after disconnect")
	}
	select {
	case err := <-requestDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client request did not cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client request did not finish")
	}
}

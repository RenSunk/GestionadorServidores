package httpx

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// SSE envía Server-Sent Events. Es seguro desde varias goroutines y, tras
// Close, ignora escrituras (p. ej. de un comando remoto que aún no terminó):
// escribir en un ResponseWriter después de que el handler retorna es un bug.
type SSE struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	rc     *http.ResponseController
	closed bool
}

func NewSSE(w http.ResponseWriter) *SSE {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // que nginx/Caddy no acumulen la respuesta
	w.WriteHeader(http.StatusOK)
	s := &SSE{w: w, rc: http.NewResponseController(w)}
	_ = s.rc.Flush()
	return s
}

// Send envía un evento. event vacío = evento "message" por defecto.
func (s *SSE) Send(event, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	var b strings.Builder
	if event != "" {
		fmt.Fprintf(&b, "event: %s\n", event)
	}
	for line := range strings.SplitSeq(data, "\n") {
		fmt.Fprintf(&b, "data: %s\n", line)
	}
	b.WriteString("\n")
	if _, err := s.w.Write([]byte(b.String())); err != nil {
		return err
	}
	return s.rc.Flush()
}

// Ping mantiene viva la conexión a través de proxies con un comentario SSE.
func (s *SSE) Ping() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if _, err := s.w.Write([]byte(": ping\n\n")); err != nil {
		return err
	}
	return s.rc.Flush()
}

func (s *SSE) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// LineWriter es un io.Writer que entrega la salida línea por línea.
type LineWriter struct {
	mu      sync.Mutex
	partial []byte
	emit    func(line string)
}

func NewLineWriter(emit func(line string)) *LineWriter {
	return &LineWriter{emit: emit}
}

func (lw *LineWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	data := append(lw.partial, p...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		lw.emit(strings.TrimRight(string(data[:i]), "\r"))
		data = data[i+1:]
	}
	lw.partial = append([]byte(nil), data...)
	return len(p), nil
}

// Flush entrega lo que quede sin salto de línea final.
func (lw *LineWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if len(lw.partial) > 0 {
		lw.emit(strings.TrimRight(string(lw.partial), "\r"))
		lw.partial = nil
	}
}

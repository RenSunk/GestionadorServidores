package projects

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"sync"
)

const maxLogBytes = 2 << 20 // 2 MB: si se supera, se descartan las líneas más antiguas

// LogBuffer acumula el log de un deploy en curso y permite que varios lectores
// (pestañas del navegador) lo sigan en vivo. Es un io.Writer seguro para usar
// desde varias goroutines.
type LogBuffer struct {
	mu      sync.Mutex
	lines   []string
	dropped int // líneas descartadas por tamaño (los cursores son absolutos)
	size    int
	partial []byte
	done    bool
	changed chan struct{} // se cierra y se reemplaza en cada cambio
}

func newLogBuffer() *LogBuffer {
	return &LogBuffer{changed: make(chan struct{})}
}

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return len(p), nil
	}
	data := append(b.partial, p...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		b.appendLine(string(data[:i]))
		data = data[i+1:]
	}
	b.partial = append([]byte(nil), data...)
	b.notify()
	return len(p), nil
}

// Step escribe un encabezado de paso ("==> Clonando...").
func (b *LogBuffer) Step(format string, args ...any) {
	fmt.Fprintf(b, "==> "+format+"\n", args...)
}

// Close marca el log como terminado; los lectores reciben el final.
func (b *LogBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return
	}
	if len(b.partial) > 0 {
		b.appendLine(string(b.partial))
		b.partial = nil
	}
	b.done = true
	b.notify()
}

// Since devuelve las líneas desde el cursor absoluto, el siguiente cursor,
// si el log terminó, y un canal que se cierra en el próximo cambio.
func (b *LogBuffer) Since(cursor int) (lines []string, next int, done bool, changed <-chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	start := max(cursor-b.dropped, 0)
	if start < len(b.lines) {
		lines = slices.Clone(b.lines[start:])
	}
	return lines, b.dropped + len(b.lines), b.done, b.changed
}

func (b *LogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := strings.Join(b.lines, "\n")
	if b.dropped > 0 {
		text = fmt.Sprintf("[... %d líneas anteriores omitidas]\n", b.dropped) + text
	}
	return text
}

func (b *LogBuffer) appendLine(line string) {
	// Las barras de progreso reescriben la línea con \r: nos quedamos con lo último.
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	b.lines = append(b.lines, line)
	b.size += len(line) + 1
	for b.size > maxLogBytes && len(b.lines) > 1 {
		b.size -= len(b.lines[0]) + 1
		b.lines = b.lines[1:]
		b.dropped++
	}
}

func (b *LogBuffer) notify() {
	close(b.changed)
	b.changed = make(chan struct{})
}

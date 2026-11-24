// Package httpx reúne utilidades HTTP compartidas por todos los módulos de la API.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("escribir respuesta json", "err", err)
	}
}

// WriteError responde {"error": "..."}; el frontend lo lee en lib/api.ts.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

const maxBodyBytes = 1 << 20 // 1 MB

// DecodeJSON lee el body en dst. Rechaza campos desconocidos y bodies enormes.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return fmt.Errorf("el cuerpo supera %d bytes", maxBodyBytes)
		case errors.Is(err, io.EOF):
			return errors.New("el cuerpo está vacío")
		default:
			return fmt.Errorf("JSON inválido: %w", err)
		}
	}
	return nil
}

// Package web embebe el build del frontend (web/dist) dentro del binario de Go
// y lo sirve como una SPA: cualquier ruta que no sea un archivo devuelve index.html,
// para que React Router resuelva la navegación en el navegador.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist siempre contiene al menos .gitkeep, así el proyecto compila aunque
// todavía no se haya hecho el build del frontend.
//
//go:embed all:dist
var dist embed.FS

// Handler devuelve el http.Handler que sirve el frontend embebido.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // solo puede fallar si cambia la directiva go:embed
	}

	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "El frontend no está compilado. Ejecuta `make web` y vuelve a compilar.", http.StatusServiceUnavailable)
		})
	}

	fileServer := http.FileServerFS(files)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if name != "" && name != "index.html" {
			if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
				// Los archivos de assets/ llevan hash en el nombre: se pueden cachear para siempre.
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Ruta de la SPA (/, /servers, /projects/123...) → index.html sin caché.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

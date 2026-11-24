package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type message struct {
	Event string `json:"event"` // alert.firing, alert.resolved, test
	Text  string `json:"text"`
	Alert *Alert `json:"alert,omitempty"`
}

type notifier struct{ client *http.Client }

func newNotifier() *notifier {
	return &notifier{client: &http.Client{Timeout: 10 * time.Second}}
}

// payload adapta el mensaje al formato de cada servicio según la URL.
func payload(url string, m message) any {
	switch {
	case strings.Contains(url, "hooks.slack.com"):
		return map[string]string{"text": m.Text}
	case strings.Contains(url, "discord.com/api/webhooks"), strings.Contains(url, "discordapp.com/api/webhooks"):
		return map[string]string{"content": m.Text}
	}
	return m // JSON genérico: {event, text, alert}
}

// send hace el POST con hasta 3 intentos.
func (n *notifier) send(ctx context.Context, url string, m message) error {
	body, err := json.Marshal(payload(url, m))
	if err != nil {
		return err
	}
	var last error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Gestionador-Alertas")
		res, err := n.client.Do(req)
		if err != nil {
			last = fmt.Errorf("no se pudo conectar con el webhook: %w", err)
			continue
		}
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		res.Body.Close()
		if res.StatusCode < 300 {
			return nil
		}
		last = fmt.Errorf("el webhook respondió %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
		if res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
			return last // error del lado del cliente: reintentar no sirve
		}
	}
	return last
}

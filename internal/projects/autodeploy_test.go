package projects

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func sign(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestVerifyHook(t *testing.T) {
	const secret, body = "s3cret", `{"ref":"refs/heads/main"}`
	cases := []struct {
		name     string
		h        http.Header
		provider string
		wantErr  bool
	}{
		{"github", hdr("X-GitHub-Event", "push", "X-Hub-Signature-256", "sha256="+sign(secret, body)), "GitHub", false},
		{"github mal", hdr("X-GitHub-Event", "push", "X-Hub-Signature-256", "sha256="+sign("otro", body)), "GitHub", true},
		{"github sin firma", hdr("X-GitHub-Event", "push"), "GitHub", true},
		{"gitea", hdr("X-Gitea-Event", "push", "X-Gitea-Signature", sign(secret, body)), "Gitea", false},
		{"forgejo", hdr("X-Forgejo-Event", "push", "X-Forgejo-Signature", sign(secret, body)), "Forgejo", false},
		{"gitlab", hdr("X-Gitlab-Event", "Push Hook", "X-Gitlab-Token", secret), "GitLab", false},
		{"gitlab mal", hdr("X-Gitlab-Event", "Push Hook", "X-Gitlab-Token", "nop"), "GitLab", true},
		{"bitbucket", hdr("X-Event-Key", "repo:push", "X-Hub-Signature", "sha256="+sign(secret, body)), "Bitbucket", false},
		{"token", hdr("X-Gestionador-Token", secret), "token", false},
	}
	for _, c := range cases {
		p, err := verifyHook(c.h, []byte(body), secret)
		if (err != nil) != c.wantErr || p != c.provider {
			t.Errorf("%s: provider=%q err=%v", c.name, p, err)
		}
		if c.wantErr && c.name == "github mal" && !errors.Is(err, errBadSignature) {
			t.Errorf("%s: se esperaba errBadSignature", c.name)
		}
	}
}

func TestParseHook(t *testing.T) {
	gh := `{"ref":"refs/heads/main","after":"abc123","pusher":{"name":"ana"},"sender":{"login":"ana-gh"}}`
	cases := []struct {
		name     string
		provider string
		h        http.Header
		body     string
		want     pushEvent
	}{
		{"github push", "GitHub", hdr("X-GitHub-Event", "push"), gh, pushEvent{Branch: "main", SHA: "abc123", Pusher: "ana-gh"}},
		{"github form", "GitHub", hdr("X-GitHub-Event", "push", "Content-Type", "application/x-www-form-urlencoded"),
			"payload=" + url.QueryEscape(gh), pushEvent{Branch: "main", SHA: "abc123", Pusher: "ana-gh"}},
		{"github ping", "GitHub", hdr("X-GitHub-Event", "ping"), `{}`, pushEvent{Ping: true}},
		{"github issue", "GitHub", hdr("X-GitHub-Event", "issues"), `{}`, pushEvent{Ignore: `evento "issues" (solo se despliega con push)`}},
		{"github tag", "GitHub", hdr("X-GitHub-Event", "push"), `{"ref":"refs/tags/v1","after":"abc"}`,
			pushEvent{SHA: "abc", Ignore: "refs/tags/v1 no es una rama"}},
		{"github borrada", "GitHub", hdr("X-GitHub-Event", "push"), `{"ref":"refs/heads/dev","after":"0000000000","deleted":true}`,
			pushEvent{Branch: "dev", SHA: "0000000000", Ignore: "se borró la rama dev"}},
		{"gitea", "Gitea", hdr("X-Gitea-Event", "push", "X-GitHub-Event", "push"),
			`{"ref":"refs/heads/main","after":"def","pusher":{"login":"luis"}}`, pushEvent{Branch: "main", SHA: "def", Pusher: "luis"}},
		{"gitlab", "GitLab", hdr("X-Gitlab-Event", "Push Hook"),
			`{"ref":"refs/heads/main","after":"f00","checkout_sha":"f00","user_username":"eva"}`, pushEvent{Branch: "main", SHA: "f00", Pusher: "eva"}},
		{"bitbucket", "Bitbucket", hdr("X-Event-Key", "repo:push"),
			`{"actor":{"nickname":"rob"},"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"b1"}}}]}}`,
			pushEvent{Branch: "main", SHA: "b1", Pusher: "rob"}},
		{"bitbucket tag", "Bitbucket", hdr("X-Event-Key", "repo:push"),
			`{"push":{"changes":[{"new":{"type":"tag","name":"v1"}}]}}`, pushEvent{Ignore: "el push no actualiza ninguna rama (tag o rama borrada)"}},
		{"token", "token", hdr(), ``, pushEvent{Pusher: "token"}},
	}
	for _, c := range cases {
		got, err := parseHook(c.provider, c.h, []byte(c.body))
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v err=%v, want %+v", c.name, got, err, c.want)
		}
	}
}

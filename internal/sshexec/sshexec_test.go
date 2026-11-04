package sshexec

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leassh-dev/leassh/internal/sshtest"
)

func target(fs *sshtest.Server) Target {
	return Target{Host: "127.0.0.1", Port: fs.Port, User: "u", Auth: Auth{PrivateKeyPEM: fs.ClientPEM}, Fingerprint: fs.Fingerprint}
}

func newFakeServer(t *testing.T) *sshtest.Server { return sshtest.New(t) }

func TestScanAndDial(t *testing.T) {
	fs := newFakeServer(t)
	fp, kt, err := ScanHostKey(context.Background(), "127.0.0.1", fs.Port)
	if err != nil || fp != fs.Fingerprint || kt != "ssh-ed25519" {
		t.Fatalf("scan: %q %q %v", fp, kt, err)
	}
	c, err := Dial(context.Background(), target(fs))
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestDialRefusals(t *testing.T) {
	fs := newFakeServer(t)
	tg := target(fs)
	tg.Fingerprint = ""
	if _, err := Dial(context.Background(), tg); err != ErrNoPinnedKey {
		t.Fatalf("sans empreinte: %v", err)
	}
	tg = target(fs)
	tg.Fingerprint = "SHA256:autre"
	_, err := Dial(context.Background(), tg)
	mm, ok := err.(*HostKeyMismatchError)
	if !ok || mm.Got != fs.Fingerprint {
		t.Fatalf("divergence attendue: %v", err)
	}
	tg = target(fs)
	other, _, _ := GenerateKey("x")
	tg.Auth.PrivateKeyPEM = other
	if _, err := Dial(context.Background(), tg); err == nil {
		t.Fatal("mauvaise clé acceptée")
	}
}

func TestRun(t *testing.T) {
	fs := newFakeServer(t)
	ctx := context.Background()
	c, err := Dial(ctx, target(fs))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	r, err := Run(ctx, c, "echo hello; echo err >&2", 5e9, 1024)
	if err != nil || r.ExitCode != 0 || strings.TrimSpace(r.Stdout) != "hello" || strings.TrimSpace(r.Stderr) != "err" || r.Truncated {
		t.Fatalf("succès: %+v %v", r, err)
	}
	r, _ = Run(ctx, c, "exit 3", 5e9, 1024)
	if r.ExitCode != 3 {
		t.Fatalf("code retour: %+v", r)
	}
	r, _ = Run(ctx, c, "head -c 5000 /dev/zero | tr '\\0' a", 5e9, 100)
	if !r.Truncated || len(r.Stdout) != 100 {
		t.Fatalf("troncature: %d %v", len(r.Stdout), r.Truncated)
	}
	r, _ = Run(ctx, c, "sleep 10", 5e8, 100)
	if !r.TimedOut || r.Duration > 5e9 {
		t.Fatalf("timeout: %+v", r)
	}
	if fs.Pty != 0 {
		t.Fatalf("%d demandes pty/shell/forwarding émises", fs.Pty)
	}
}

func TestPoolReuseAndConcurrency(t *testing.T) {
	fs := newFakeServer(t)
	p := NewPool(time2s, 2)
	defer p.Close()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Exec(context.Background(), "srv", target(fs), "sleep 0.2; echo ok", 5e9, 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if fs.Max > 2 {
		t.Fatalf("concurrence %d > 2", fs.Max)
	}
	p.mu.Lock()
	n := len(p.conns)
	p.mu.Unlock()
	if n != 1 {
		t.Fatalf("connexions poolées: %d", n)
	}
}

const time2s = 2e9

func TestPublicKeyOfExplainsPasteMistakes(t *testing.T) {
	priv, pub, _ := GenerateKey("t")
	got, err := PublicKeyOf(priv)
	if err != nil || got != pub {
		t.Fatalf("clé valide: %v", err)
	}
	// fins de ligne Windows et absence de retour final: acceptées
	crlf := strings.ReplaceAll(strings.TrimSpace(string(priv)), "\n", "\r\n")
	if _, err := PublicKeyOf([]byte(crlf)); err != nil {
		t.Fatalf("CRLF: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(priv)), "\n")
	cases := map[string]struct{ in, want string }{
		"vide":             {"  ", "aucune clé"},
		"clé publique":     {pub, "clé publique"},
		"corps sans BEGIN": {strings.Join(lines[1:len(lines)-1], "\n"), "-----BEGIN"},
		"sans END":         {strings.Join(lines[:len(lines)-1], "\n"), "tronquée"},
	}
	for name, c := range cases {
		if _, err := PublicKeyOf([]byte(c.in)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: erreur contenant %q attendue, obtenu %v", name, c.want, err)
		}
	}
}

// Après un échec d'authentification, on n'insiste pas: un bastion (Warpgate…) bloquerait l'IP de Leassh.
func TestAuthFailureCooldown(t *testing.T) {
	fs := newFakeServer(t)
	p := NewPool(time.Minute, 2)
	defer p.Close()
	p.AuthCooldown = 300 * time.Millisecond
	other, _, _ := GenerateKey("autre")
	bad := target(fs)
	bad.Auth.PrivateKeyPEM = other
	ctx := context.Background()
	if _, err := p.Exec(ctx, "s", bad, "echo hi", 5*time.Second, 100); !IsAuthFailure(err) {
		t.Fatalf("échec d'authentification attendu: %v", err)
	}
	fs.Lock()
	before := fs.Accepted
	fs.Unlock()
	for i := 0; i < 5; i++ {
		_, err := p.Exec(ctx, "s", bad, "echo hi", 5*time.Second, 100)
		var cd *CooldownError
		if !errors.As(err, &cd) {
			t.Fatalf("tentative %d: pause attendue, obtenu %v", i, err)
		}
	}
	fs.Lock()
	after := fs.Accepted
	fs.Unlock()
	if after != before {
		t.Fatalf("%d connexions pendant la pause (0 attendue)", after-before)
	}
	// une modification de la configuration du serveur lève la pause
	p.Invalidate("s")
	if res, err := p.Exec(ctx, "s", target(fs), "echo ok", 5*time.Second, 100); err != nil || strings.TrimSpace(res.Stdout) != "ok" {
		t.Fatalf("après Invalidate: %v %+v", err, res)
	}
	// et la pause expire d'elle-même
	p2 := NewPool(time.Minute, 2)
	defer p2.Close()
	p2.AuthCooldown = 100 * time.Millisecond
	p2.Exec(ctx, "s", bad, "echo hi", 5*time.Second, 100)
	time.Sleep(150 * time.Millisecond)
	if _, err := p2.Exec(ctx, "s", bad, "echo hi", 5*time.Second, 100); !IsAuthFailure(err) {
		t.Fatalf("après expiration, nouvel essai attendu: %v", err)
	}
}

// Package sshexec: connexion SSH, vérification stricte de la clé d'hôte, exécution via canal exec uniquement.
// Invariant 3: jamais de PTY, de shell, d'agent, de forwarding de ports ni de X11.
package sshexec

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// HostKeyMismatchError: la clé présentée diffère de l'empreinte épinglée.
type HostKeyMismatchError struct{ Expected, Got string }

func (e *HostKeyMismatchError) Error() string {
	return fmt.Sprintf("clé d'hôte divergente: épinglée %s, reçue %s", e.Expected, e.Got)
}

// ErrNoPinnedKey: refus de se connecter sans empreinte épinglée.
var ErrNoPinnedKey = errors.New("aucune clé d'hôte épinglée")

// ErrOpenChannel: échec avant le démarrage de la commande (seul cas où un nouvel essai est sans risque).
var ErrOpenChannel = errors.New("ouverture du canal")

// errScanDone interrompt la poignée de main une fois la clé d'hôte capturée.
var errScanDone = errors.New("scan terminé")

type Auth struct {
	PrivateKeyPEM []byte
	Password      string
}

type Target struct {
	Host        string
	Port        int
	User        string
	Auth        Auth
	Fingerprint string // SHA256:… épinglée (obligatoire pour Dial)
}

func (t Target) addr() string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

// GenerateKey crée une paire ed25519: clé privée au format OpenSSH (PEM) et clé publique authorized_keys.
func GenerateKey(comment string) (privPEM []byte, authorized string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("génération ed25519: %w", err)
	}
	blk, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, "", fmt.Errorf("encodage clé privée: %w", err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", fmt.Errorf("clé publique: %w", err)
	}
	return pem.EncodeToMemory(blk), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))), nil
}

// PublicKeyOf valide une clé privée importée (sans passphrase) et renvoie sa clé publique.
// Les erreurs fréquentes de copier-coller (en-tête manquant, clé publique collée) reçoivent un message explicite.
func PublicKeyOf(privPEM []byte) (string, error) {
	txt := strings.TrimSpace(strings.ReplaceAll(string(privPEM), "\r\n", "\n"))
	switch {
	case txt == "":
		return "", errors.New("aucune clé fournie")
	case strings.HasPrefix(txt, "ssh-") || strings.HasPrefix(txt, "ecdsa-") || strings.HasPrefix(txt, "sk-"):
		return "", errors.New("ceci est une clé publique: il faut la clé privée (fichier id_ed25519, sans l'extension .pub)")
	case !strings.Contains(txt, "-----BEGIN"):
		return "", errors.New("la clé doit être collée en entier, de la ligne « -----BEGIN … PRIVATE KEY----- » à la ligne « -----END … PRIVATE KEY----- » incluses")
	case !strings.Contains(txt, "-----END"):
		return "", errors.New("la clé est tronquée: la ligne « -----END … PRIVATE KEY----- » est absente")
	}
	s, err := ssh.ParsePrivateKey([]byte(txt + "\n"))
	if err != nil {
		return "", fmt.Errorf("clé privée invalide (les clés protégées par passphrase ne sont pas supportées): %w", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))), nil
}

func (a Auth) methods() ([]ssh.AuthMethod, error) {
	var m []ssh.AuthMethod
	if len(a.PrivateKeyPEM) > 0 {
		s, err := ssh.ParsePrivateKey(a.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("clé privée: %w", err)
		}
		m = append(m, ssh.PublicKeys(s))
	}
	if a.Password != "" {
		m = append(m, ssh.Password(a.Password))
	}
	if len(m) == 0 {
		return nil, errors.New("aucune méthode d'authentification")
	}
	return m, nil
}

const dialTimeout = 10 * time.Second

func dial(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connexion TCP: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// ScanHostKey récupère l'empreinte présentée par l'hôte sans s'authentifier ni envoyer de secret.
func ScanHostKey(ctx context.Context, host string, port int) (fingerprint, keyType string, err error) {
	cfg := &ssh.ClientConfig{
		User: "leassh-scan",
		Auth: []ssh.AuthMethod{ssh.Password("")},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			fingerprint, keyType = ssh.FingerprintSHA256(k), k.Type()
			return errScanDone
		},
		Timeout: dialTimeout,
	}
	_, err = dial(ctx, net.JoinHostPort(host, strconv.Itoa(port)), cfg)
	if fingerprint != "" {
		return fingerprint, keyType, nil
	}
	if err == nil {
		err = errors.New("clé d'hôte non reçue")
	}
	return "", "", err
}

// Dial ouvre une connexion authentifiée en exigeant la clé d'hôte épinglée.
func Dial(ctx context.Context, t Target) (*ssh.Client, error) {
	if t.Fingerprint == "" {
		return nil, ErrNoPinnedKey
	}
	methods, err := t.Auth.methods()
	if err != nil {
		return nil, err
	}
	var mismatch *HostKeyMismatchError
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: methods,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(k); got != t.Fingerprint {
				mismatch = &HostKeyMismatchError{Expected: t.Fingerprint, Got: got}
				return mismatch
			}
			return nil
		},
		Timeout: dialTimeout,
	}
	c, err := dial(ctx, t.addr(), cfg)
	if mismatch != nil {
		return nil, mismatch
	}
	if err != nil {
		return nil, fmt.Errorf("ssh %s: %w", t.addr(), err)
	}
	return c, nil
}

type Result struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	Truncated bool
	TimedOut  bool
	Duration  time.Duration
}

type capWriter struct {
	mu        sync.Mutex
	buf       []byte
	max       int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := w.max - len(w.buf); room > 0 {
		if len(p) > room {
			w.buf = append(w.buf, p[:room]...)
			w.truncated = true
		} else {
			w.buf = append(w.buf, p...)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil // on continue à lire pour ne pas bloquer le distant
}

func (w *capWriter) result() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.ToValidUTF8(string(w.buf), "�"), w.truncated
}

var _ io.Writer = (*capWriter)(nil)

// Run exécute cmd via un canal exec (jamais de PTY ni de shell interactif).
// À l'expiration du délai: SIGTERM puis fermeture du canal, TimedOut=true.
func Run(ctx context.Context, client *ssh.Client, cmd string, timeout time.Duration, maxBytes int) (Result, error) {
	sess, err := client.NewSession()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrOpenChannel, err)
	}
	defer sess.Close()
	out, errw := &capWriter{max: maxBytes}, &capWriter{max: maxBytes}
	sess.Stdout, sess.Stderr = out, errw
	start := time.Now()
	if err := sess.Start(cmd); err != nil {
		return Result{}, fmt.Errorf("démarrage: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	res := Result{}
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(timeout):
		res.TimedOut = true
	case <-ctx.Done():
		res.TimedOut = true
	}
	if res.TimedOut {
		_ = sess.Signal(ssh.SIGTERM)
		select {
		case waitErr = <-done:
		case <-time.After(2 * time.Second):
		}
		sess.Close()
	}
	res.Duration = time.Since(start)
	var so, se bool
	res.Stdout, so = out.result()
	res.Stderr, se = errw.result()
	res.Truncated = so || se
	var ee *ssh.ExitError
	switch {
	case res.TimedOut:
		res.ExitCode = -1
	case waitErr == nil:
		res.ExitCode = 0
	case errors.As(waitErr, &ee):
		res.ExitCode = ee.ExitStatus()
	default:
		return res, fmt.Errorf("attente: %w", waitErr)
	}
	return res, nil
}

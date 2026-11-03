// Package sshtest fournit un serveur SSH en processus pour les tests (exécute via `sh -c` local).
// Réservé aux tests: ne jamais importer depuis le code de production.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os/exec"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Server est un serveur SSH en processus (tests uniquement): exécute la commande via `sh -c` local.
type Server struct {
	Addr        net.Addr
	Fingerprint string
	ClientPEM   []byte
	Port        int
	mu          sync.Mutex
	active, Max int
	Accepted    int    // connexions TCP acceptées
	LastUser    string // utilisateur présenté lors de la dernière authentification
	Pty         int    // nombre de demandes pty/shell/forwarding reçues (doit rester 0)
}

func New(t testing.TB) *Server {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)
	_, cpriv, _ := ed25519.GenerateKey(rand.Reader)
	blk, _ := ssh.MarshalPrivateKey(cpriv, "test")
	clientPEM := pem.EncodeToMemory(blk)
	csigner, _ := ssh.NewSignerFromKey(cpriv)
	authorized := csigner.PublicKey()
	var fsRef *Server
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == string(authorized.Marshal()) {
			fsRef.mu.Lock()
			fsRef.LastUser = c.User()
			fsRef.mu.Unlock()
			return nil, nil
		}
		return nil, ssh.ErrNoAuth
	}}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	fs := &Server{Addr: ln.Addr(), Fingerprint: ssh.FingerprintSHA256(hostSigner.PublicKey()), ClientPEM: clientPEM, Port: ln.Addr().(*net.TCPAddr).Port}
	fsRef = fs
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			fs.mu.Lock()
			fs.Accepted++
			fs.mu.Unlock()
			go fs.serve(c, cfg)
		}
	}()
	return fs
}

// Lock / Unlock protègent LastUser et les compteurs pour les tests qui les lisent.
func (f *Server) Lock()   { f.mu.Lock() }
func (f *Server) Unlock() { f.mu.Unlock() }

func (f *Server) serve(c net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "non supporté")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer ch.Close()
			var cmd *exec.Cmd
			var cmu sync.Mutex
			for r := range creqs {
				switch r.Type {
				case "exec":
					n := int(r.Payload[3])
					line := string(r.Payload[4 : 4+n])
					r.Reply(true, nil)
					f.mu.Lock()
					f.active++
					if f.active > f.Max {
						f.Max = f.active
					}
					f.mu.Unlock()
					c := exec.Command("sh", "-c", line)
					c.Stdout, c.Stderr = ch, ch.Stderr()
					startErr := c.Start()
					cmu.Lock()
					cmd = c
					cmu.Unlock()
					go func() {
						err := startErr
						if err == nil {
							err = c.Wait()
						}
						f.mu.Lock()
						f.active--
						f.mu.Unlock()
						code := 0
						if ee, ok := err.(*exec.ExitError); ok {
							code = ee.ExitCode()
						}
						ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)}))
						ch.Close()
					}()
				case "signal":
					cmu.Lock()
					if cmd != nil && cmd.Process != nil {
						cmd.Process.Kill()
					}
					cmu.Unlock()
					r.Reply(true, nil)
				default: // pty-req, shell, env, x11-req, auth-agent-req…
					f.mu.Lock()
					f.Pty++
					f.mu.Unlock()
					r.Reply(false, nil)
				}
			}
		}()
	}
}

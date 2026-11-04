// Package syslogtest fournit un serveur syslog en mémoire (UDP, TCP ou TCP+TLS) pour les tests.
// Réservé aux tests: ne jamais importer depuis le code de production.
package syslogtest

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type Server struct {
	t     testing.TB
	Proto string // udp | tcp | tls
	Host  string
	Port  int
	CAPEM string // autorité à donner à Leassh (tls)

	mu    sync.Mutex
	msgs  []string
	ln    net.Listener
	pc    net.PacketConn
	conns []net.Conn
	cert  tls.Certificate
	// ClientCAs: si renseigné, le serveur exige un certificat client signé par cette autorité.
	ClientCAs *x509.CertPool
}

// New démarre un serveur sur 127.0.0.1 (port libre).
func New(t testing.TB, proto string) *Server {
	t.Helper()
	s := &Server{t: t, Proto: proto, Host: "127.0.0.1"}
	if proto == "tls" {
		s.CAPEM, s.cert = selfSigned(t)
	}
	s.listen(0)
	t.Cleanup(s.Stop)
	return s
}

func (s *Server) listen(port int) {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(port))
	var err error
	if s.Proto == "udp" {
		s.pc, err = net.ListenPacket("udp", addr)
		if err != nil {
			s.t.Fatal(err)
		}
		s.Port = s.pc.LocalAddr().(*net.UDPAddr).Port
		go s.serveUDP(s.pc)
		return
	}
	var ln net.Listener
	for i := 0; i < 50; i++ { // le port peut mettre un instant à se libérer au redémarrage
		if ln, err = net.Listen("tcp", addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		s.t.Fatal(err)
	}
	if s.Proto == "tls" {
		cfg := &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12}
		if s.ClientCAs != nil {
			cfg.ClientAuth, cfg.ClientCAs = tls.RequireAndVerifyClientCert, s.ClientCAs
		}
		ln = tls.NewListener(ln, cfg)
	}
	s.ln = ln
	s.Port = ln.Addr().(*net.TCPAddr).Port
	go s.serveTCP(ln)
}

func (s *Server) add(m string) {
	s.mu.Lock()
	s.msgs = append(s.msgs, m)
	s.mu.Unlock()
}

func (s *Server) serveUDP(pc net.PacketConn) {
	buf := make([]byte, 70000)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		s.add(string(buf[:n]))
	}
}

func (s *Server) serveTCP(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		go s.readStream(c)
	}
}

// readStream: cadrage détecté au premier octet ('<' = une ligne par message, chiffre = octet counting, RFC 6587).
func (s *Server) readStream(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		b, err := r.Peek(1)
		if err != nil {
			return
		}
		if b[0] >= '0' && b[0] <= '9' {
			lenStr, err := r.ReadString(' ')
			if err != nil {
				return
			}
			n, err := strconv.Atoi(strings.TrimSpace(lenStr))
			if err != nil {
				return
			}
			msg := make([]byte, n)
			if _, err := io.ReadFull(r, msg); err != nil {
				return
			}
			s.add(string(msg))
			continue
		}
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		s.add(strings.TrimSuffix(line, "\n"))
	}
}

// Messages renvoie une copie des messages reçus.
func (s *Server) Messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

// WaitFor attend au moins n messages (5 s au plus) et les renvoie.
func (s *Server) WaitFor(n int) []string {
	s.t.Helper()
	for i := 0; i < 250; i++ {
		if m := s.Messages(); len(m) >= n {
			return m
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("syslogtest: %d message(s) reçu(s), %d attendu(s): %q", len(s.Messages()), n, s.Messages())
	return nil
}

// DropConns ferme les connexions entrantes (le serveur continue d'écouter).
func (s *Server) DropConns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
}

// Stop arrête l'écoute et ferme les connexions. Start relance sur le même port.
func (s *Server) Stop() {
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
	}
	if s.pc != nil {
		_ = s.pc.Close()
		s.pc = nil
	}
	s.DropConns()
}

func (s *Server) Start() { s.listen(s.Port) }

func selfSigned(t testing.TB) (string, tls.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "syslog-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true,
		DNSNames: []string{"syslog.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// ClientCert génère une paire certificat/clé cliente (PEM) et l'autorité à placer dans ClientCAs.
func ClientCert(t testing.TB) (certPEM, keyPEM string, cas *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "leassh-client"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalPKCS8PrivateKey(key)
	crt, _ := x509.ParseCertificate(der)
	cas = x509.NewCertPool()
	cas.AddCert(crt)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})), cas
}

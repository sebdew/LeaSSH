package syslog

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const ioTimeout = 5 * time.Second

// conn est une connexion vers le serveur syslog (un datagramme UDP par message, ou un flux TCP/TLS cadré).
type conn struct {
	c       net.Conn
	stream  bool
	octet   bool
	maxSize int
}

func dial(ctx context.Context, r *runtime) (*conn, error) {
	addr := net.JoinHostPort(r.cfg.Host, strconv.Itoa(r.cfg.Port))
	d := &net.Dialer{Timeout: ioTimeout}
	var nc net.Conn
	var err error
	switch r.cfg.Protocol {
	case "udp":
		nc, err = d.DialContext(ctx, "udp", addr)
	case "tcp":
		nc, err = d.DialContext(ctx, "tcp", addr)
	case "tls":
		td := &tls.Dialer{NetDialer: d, Config: r.tls}
		nc, err = td.DialContext(ctx, "tcp", addr)
	default:
		err = fmt.Errorf("protocole %q", r.cfg.Protocol)
	}
	if err != nil {
		return nil, err
	}
	return &conn{c: nc, stream: r.cfg.Protocol != "udp", octet: r.cfg.Framing == "octet", maxSize: 60000}, nil
}

func (c *conn) Close() error { return c.c.Close() }

// alive détecte une fermeture côté serveur avant l'écriture (un flux TCP rompu accepte sinon le premier envoi en silence).
func (c *conn) alive() bool {
	if !c.stream {
		return true
	}
	_ = c.c.SetReadDeadline(time.Now().Add(time.Millisecond))
	var b [1]byte
	_, err := c.c.Read(b[:])
	_ = c.c.SetReadDeadline(time.Time{})
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout() && !errors.Is(err, io.EOF)
}

func (c *conn) send(msg []byte) error {
	if len(msg) > c.maxSize {
		msg = msg[:c.maxSize]
	}
	_ = c.c.SetWriteDeadline(time.Now().Add(ioTimeout))
	switch {
	case !c.stream:
		_, err := c.c.Write(msg)
		return err
	case c.octet:
		_, err := c.c.Write(append([]byte(strconv.Itoa(len(msg))+" "), msg...))
		return err
	default:
		_, err := c.c.Write(append(append(make([]byte, 0, len(msg)+1), msg...), '\n'))
		return err
	}
}

// confirm laisse un instant au serveur pour refuser la connexion (TLS 1.3 signale un certificat client refusé après la poignée de main).
func (c *conn) confirm(wait time.Duration) error {
	if !c.stream {
		return nil
	}
	_ = c.c.SetReadDeadline(time.Now().Add(wait))
	var b [1]byte
	_, err := c.c.Read(b[:])
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return nil
	}
	if err == nil {
		return nil
	}
	return err
}

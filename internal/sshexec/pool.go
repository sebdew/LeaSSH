package sshexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type poolEntry struct {
	client   *ssh.Client
	lastUsed time.Time
}

// Pool garde des connexions à durée de vie courte et borne la concurrence par serveur.
type Pool struct {
	mu        sync.Mutex
	conns     map[string]*poolEntry
	sems      map[string]chan struct{}
	idle      time.Duration
	perServer int
	stop      chan struct{}
	cool      map[string]time.Time // serveur → fin de la pause après un échec d'authentification
	// AuthCooldown: pause avant toute nouvelle tentative sur un serveur dont l'authentification a échoué.
	// Évite de faire bloquer l'IP de Leash par un bastion (Warpgate…) ou un fail2ban à force de réessais.
	AuthCooldown time.Duration
}

// CooldownError: tentative refusée localement, le serveur est en pause après un échec d'authentification.
type CooldownError struct{ Until time.Time }

func (e *CooldownError) Error() string {
	return fmt.Sprintf("pause de %d s après un échec d'authentification (évite de faire bloquer l'IP de Leash)", int(time.Until(e.Until).Seconds())+1)
}

// IsAuthFailure reconnaît l'échec d'authentification renvoyé par x/crypto/ssh.
func IsAuthFailure(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unable to authenticate")
}

// NewPool: idle = durée d'inactivité avant fermeture; perServer = exécutions simultanées max par serveur.
func NewPool(idle time.Duration, perServer int) *Pool {
	p := &Pool{conns: map[string]*poolEntry{}, sems: map[string]chan struct{}{}, idle: idle, perServer: perServer, stop: make(chan struct{}), cool: map[string]time.Time{}, AuthCooldown: 60 * time.Second}
	go p.janitor()
	return p
}

func (p *Pool) janitor() {
	t := time.NewTicker(p.idle / 2)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			for k, e := range p.conns {
				if time.Since(e.lastUsed) > p.idle {
					e.client.Close()
					delete(p.conns, k)
				}
			}
			p.mu.Unlock()
		}
	}
}

func (p *Pool) Close() {
	close(p.stop)
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, e := range p.conns {
		e.client.Close()
		delete(p.conns, k)
	}
}

// Invalidate ferme la connexion d'un serveur (changement de configuration, divergence de clé).
func (p *Pool) Invalidate(serverID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.cool, serverID) // une modification de configuration lève la pause
	if e, ok := p.conns[serverID]; ok {
		e.client.Close()
		delete(p.conns, serverID)
	}
}

func (p *Pool) sem(id string) chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sems[id]
	if !ok {
		s = make(chan struct{}, p.perServer)
		p.sems[id] = s
	}
	return s
}

func (p *Pool) get(ctx context.Context, id string, t Target) (*ssh.Client, error) {
	p.mu.Lock()
	if e, ok := p.conns[id]; ok && time.Since(e.lastUsed) <= p.idle {
		e.lastUsed = time.Now()
		p.mu.Unlock()
		return e.client, nil
	}
	p.mu.Unlock()
	c, err := Dial(ctx, t)
	if err != nil {
		if IsAuthFailure(err) && p.AuthCooldown > 0 {
			p.mu.Lock()
			p.cool[id] = time.Now().Add(p.AuthCooldown)
			p.mu.Unlock()
		}
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.conns[id]; ok && time.Since(e.lastUsed) <= p.idle {
		c.Close() // une autre goroutine a déjà établi la connexion: on la réutilise
		e.lastUsed = time.Now()
		return e.client, nil
	}
	p.conns[id] = &poolEntry{client: c, lastUsed: time.Now()}
	return c, nil
}

// Exec exécute cmd sur le serveur id. La clé de pool est l'id du serveur: toute modification de sa
// configuration doit appeler Invalidate. Une connexion périmée est rétablie de façon transparente.
func (p *Pool) Exec(ctx context.Context, id string, t Target, cmd string, timeout time.Duration, maxBytes int) (Result, error) {
	p.mu.Lock()
	until, cooling := p.cool[id]
	p.mu.Unlock()
	if cooling && time.Now().Before(until) {
		return Result{}, &CooldownError{Until: until}
	}
	sem := p.sem(id)
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	for attempt := 0; ; attempt++ {
		c, err := p.get(ctx, id, t)
		if err != nil {
			return Result{}, err
		}
		res, err := Run(ctx, c, cmd, timeout, maxBytes)
		if err == nil {
			return res, nil
		}
		p.drop(id, c)
		// Nouvel essai uniquement si la commande n'a pas pu démarrer: jamais de double exécution.
		if attempt >= 1 || !errors.Is(err, ErrOpenChannel) {
			return res, fmt.Errorf("exécution: %w", err)
		}
	}
}

// drop ferme la connexion uniquement si c'est toujours celle qui a échoué.
func (p *Pool) drop(id string, c *ssh.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.conns[id]; ok && e.client == c {
		delete(p.conns, id)
	}
	c.Close()
}

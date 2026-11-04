package syslog

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/leassh-dev/leassh/internal/crypto"
	"github.com/leassh-dev/leassh/internal/store"
)

// Status: état de l'export, affiché dans l'interface.
type Status struct {
	State         string `json:"state"` // disabled | idle | connected | retrying
	Queued        int    `json:"queued"`
	Sent          int64  `json:"sent"`
	Dropped       int64  `json:"dropped"`
	LastError     string `json:"last_error"`
	LastErrorAt   string `json:"last_error_at"`
	LastSuccessAt string `json:"last_success_at"`
}

type item struct {
	n  uint64
	ev Event
}

// Forwarder met les événements en file (mémoire, bornée) et les envoie en arrière-plan avec reconnexion.
// Push ne bloque jamais: si la file est pleine, l'événement le plus ancien est abandonné (et compté).
type Forwarder struct {
	st  *store.Store
	box *crypto.Box
	log *slog.Logger

	Version string
	// Réglages modifiables avant Run (tests).
	MaxQueue   int
	MinBackoff time.Duration
	MaxBackoff time.Duration
	AuditPoll  time.Duration

	cfg  atomic.Pointer[runtime]
	gen  atomic.Uint64
	wake chan struct{}

	mu          sync.Mutex
	queue       []item
	seq         uint64
	sent        int64
	dropped     int64
	connected   bool
	lastErr     string
	lastErrAt   time.Time
	lastOKAt    time.Time
	deliveredAt int64 // dernier seq d'audit livré, pas encore enregistré
	sinceFlush  int
}

func New(st *store.Store, box *crypto.Box, version string, log *slog.Logger) *Forwarder {
	return &Forwarder{st: st, box: box, log: log, Version: version, MaxQueue: 10000, MinBackoff: time.Second, MaxBackoff: time.Minute,
		AuditPoll: 2 * time.Second, wake: make(chan struct{}, 1)}
}

func (f *Forwarder) signal() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// Reload relit la configuration en base; à appeler au démarrage et après chaque modification.
func (f *Forwarder) Reload(ctx context.Context) error {
	sc, err := store.GetSyslogConfig(ctx, f.st.DB)
	if err != nil {
		return err
	}
	rt, err := f.build(sc)
	if err != nil {
		f.cfg.Store(nil)
		f.setError("configuration invalide: " + err.Error())
		return err
	}
	rt.gen = f.gen.Add(1)
	f.cfg.Store(rt)
	f.mu.Lock()
	if !rt.cfg.Enabled {
		f.queue, f.connected = nil, false
	}
	f.mu.Unlock()
	f.signal()
	return nil
}

// Push met un événement en file s'il passe le filtre. Sans effet si l'export est désactivé.
func (f *Forwarder) Push(e Event) {
	rt := f.cfg.Load()
	if rt == nil || !rt.allows(e) {
		return
	}
	f.mu.Lock()
	if f.MaxQueue > 0 && len(f.queue) >= f.MaxQueue {
		f.queue = f.queue[1:]
		f.dropped++
	}
	f.seq++
	f.queue = append(f.queue, item{f.seq, e})
	f.mu.Unlock()
	f.signal()
}

// OnExecution / OnProposal se branchent sur executions.Service.Observe et proposals.Service.Notify.
func (f *Forwarder) OnExecution(event string, e store.Execution) { f.Push(FromExecution(event, e)) }
func (f *Forwarder) OnProposal(event string, p store.Proposal)   { f.Push(FromProposal(event, p)) }

func (f *Forwarder) setError(msg string) {
	f.mu.Lock()
	f.lastErr, f.lastErrAt, f.connected = msg, time.Now(), false
	f.mu.Unlock()
}

// Status décrit l'état courant.
func (f *Forwarder) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := Status{Queued: len(f.queue), Sent: f.sent, Dropped: f.dropped, LastError: f.lastErr}
	if !f.lastErrAt.IsZero() {
		s.LastErrorAt = store.FormatTime(f.lastErrAt)
	}
	if !f.lastOKAt.IsZero() {
		s.LastSuccessAt = store.FormatTime(f.lastOKAt)
	}
	switch rt := f.cfg.Load(); {
	case rt == nil || !rt.cfg.Enabled:
		s.State = "disabled"
	case f.connected:
		s.State = "connected"
	case f.lastErr != "" && !f.lastErrAt.Before(f.lastOKAt):
		s.State = "retrying"
	default:
		s.State = "idle"
	}
	return s
}

func (f *Forwarder) head() (item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return item{}, false
	}
	return f.queue[0], true
}

// delivered retire l'élément livré (s'il n'a pas été abandonné entre-temps) et mémorise le curseur d'audit.
func (f *Forwarder) delivered(ctx context.Context, it item) {
	f.mu.Lock()
	if len(f.queue) > 0 && f.queue[0].n == it.n {
		f.queue = f.queue[1:]
	}
	f.sent++
	f.connected = true
	f.lastOKAt = time.Now()
	if it.ev.AuditSeq > f.deliveredAt {
		f.deliveredAt = it.ev.AuditSeq
		f.sinceFlush++
	}
	flush := f.deliveredAt > 0 && (len(f.queue) == 0 || f.sinceFlush >= 50)
	seq := f.deliveredAt
	if flush {
		f.sinceFlush = 0
	}
	f.mu.Unlock()
	if flush {
		if err := store.SetSyslogAuditCursor(context.WithoutCancel(ctx), f.st.DB, seq); err != nil {
			f.log.Error("curseur syslog", "err", err)
		}
	}
}

func (f *Forwarder) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-f.wake:
	case <-t.C:
	}
}

// Run envoie la file jusqu'à l'annulation du contexte, et suit le journal d'administration.
func (f *Forwarder) Run(ctx context.Context) {
	go f.auditLoop(ctx)
	var c *conn
	var cgen uint64
	closeConn := func() {
		if c != nil {
			_ = c.Close()
			c = nil
		}
	}
	defer closeConn()
	attempt := 0
	for ctx.Err() == nil {
		rt := f.cfg.Load()
		if rt == nil || !rt.cfg.Enabled {
			closeConn()
			f.mu.Lock()
			f.connected = false
			f.mu.Unlock()
			f.sleep(ctx, time.Hour)
			continue
		}
		if c != nil && cgen != rt.gen {
			closeConn() // la configuration a changé: nouvelle connexion
		}
		it, ok := f.head()
		if !ok {
			f.sleep(ctx, time.Hour)
			continue
		}
		if c != nil && !c.alive() {
			closeConn()
			f.setError("connexion fermée par le serveur")
		}
		if c == nil {
			var err error
			if c, err = dial(ctx, rt); err != nil {
				c = nil
				f.setError("connexion: " + err.Error())
				f.sleep(ctx, f.backoff(attempt))
				attempt++
				continue
			}
			cgen = rt.gen
		}
		if err := c.send(rt.Render(it.ev)); err != nil {
			closeConn()
			f.setError("envoi: " + err.Error())
			f.sleep(ctx, f.backoff(attempt))
			attempt++
			continue
		}
		attempt = 0
		f.delivered(ctx, it)
	}
}

func (f *Forwarder) backoff(attempt int) time.Duration {
	d := f.MinBackoff
	for i := 0; i < attempt && d < f.MaxBackoff; i++ {
		d *= 2
	}
	return min(d, f.MaxBackoff)
}

// auditLoop transmet les événements d'administration validés en base (après commit), à partir du curseur de livraison.
func (f *Forwarder) auditLoop(ctx context.Context) {
	next := int64(-1)
	t := time.NewTicker(f.AuditPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rt := f.cfg.Load()
		if rt == nil || !rt.cfg.Enabled {
			next = -1
			continue
		}
		if next < 0 {
			n, err := f.initCursor(ctx)
			if err != nil {
				f.log.Error("curseur syslog", "err", err)
				continue
			}
			next = n
		}
		for {
			evs, err := store.AuditAfter(ctx, f.st.DB, next, 200)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					f.log.Error("lecture de l'audit pour syslog", "err", err)
				}
				break
			}
			for _, a := range evs {
				next = a.Seq
				f.Push(FromAudit(a))
			}
			if len(evs) < 200 {
				break
			}
		}
	}
}

// initCursor: reprend au curseur enregistré; sans curseur, part de la fin du journal.
func (f *Forwarder) initCursor(ctx context.Context) (int64, error) {
	sc, err := store.GetSyslogConfig(ctx, f.st.DB)
	if err != nil {
		return 0, err
	}
	if sc.AuditCursor.Valid {
		return sc.AuditCursor.Int64, nil
	}
	n, err := store.MaxAuditSeq(ctx, f.st.DB)
	if err != nil {
		return 0, err
	}
	return n, store.SetSyslogAuditCursor(ctx, f.st.DB, n)
}

// SendTest envoie un message de test immédiat (hors file) avec la configuration enregistrée, même si l'export est désactivé.
func (f *Forwarder) SendTest(ctx context.Context) error {
	sc, err := store.GetSyslogConfig(ctx, f.st.DB)
	if err != nil {
		return err
	}
	if sc.Host == "" {
		return invalid("renseignez l'adresse du serveur avant de tester")
	}
	rt, err := f.build(sc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*ioTimeout)
	defer cancel()
	c, err := dial(ctx, rt)
	if err != nil {
		return errors.New("connexion: " + err.Error())
	}
	defer c.Close()
	ev := Event{Time: time.Now(), Type: "syslog.test", Severity: sevInfo, Fields: []Field{{"message", "Message de test envoyé depuis Leassh"}}}
	if err := c.send(rt.Render(ev)); err != nil {
		return errors.New("envoi: " + err.Error())
	}
	if err := c.confirm(300 * time.Millisecond); err != nil {
		return errors.New("refusé par le serveur: " + err.Error())
	}
	return nil
}

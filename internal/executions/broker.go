package executions

import "sync"

// Broker diffuse les identifiants d'exécutions modifiées (attente longue de get_execution, SSE du journal).
type Broker struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewBroker() *Broker { return &Broker{subs: map[chan string]struct{}{}} }

func (b *Broker) Publish(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.subs {
		select {
		case c <- id:
		default: // abonné lent: il rattrapera par relecture
		}
	}
}

func (b *Broker) Subscribe() (<-chan string, func()) {
	c := make(chan string, 64)
	b.mu.Lock()
	b.subs[c] = struct{}{}
	b.mu.Unlock()
	return c, func() {
		b.mu.Lock()
		delete(b.subs, c)
		b.mu.Unlock()
	}
}

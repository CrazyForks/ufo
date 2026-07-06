package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// runQueuedChannel wakes long-poll accept requests (rover assignment).
	runQueuedChannel = "ufo_run_queued"
	// changedChannel wakes live UI streams on visible changes.
	changedChannel = "ufo_changed"
)

type Notification struct {
	Channel string
	Payload string
}

type Notifier struct {
	databaseURL string
	channels    []string
	mu          sync.Mutex
	subs        map[chan Notification]map[string]bool
}

func NewNotifier(databaseURL string, channels ...string) *Notifier {
	return &Notifier{
		databaseURL: databaseURL,
		channels:    channels,
		subs:        make(map[chan Notification]map[string]bool),
	}
}

func (n *Notifier) Subscribe(channels ...string) (<-chan Notification, func()) {
	want := make(map[string]bool, len(channels))
	for _, c := range channels {
		want[c] = true
	}
	ch := make(chan Notification, 8)
	n.mu.Lock()
	n.subs[ch] = want
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		if _, ok := n.subs[ch]; ok {
			delete(n.subs, ch)
			close(ch)
		}
		n.mu.Unlock()
	}
}

func (n *Notifier) broadcast(note Notification) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch, want := range n.subs {
		if !want[note.Channel] {
			continue
		}
		select {
		case ch <- note:
		default: // slow consumer: drop (a later notification will re-sync)
		}
	}
}

func (n *Notifier) Start(ctx context.Context) {
	go func() {
		for ctx.Err() == nil {
			if err := n.listen(ctx); err != nil && ctx.Err() == nil {
				log.Printf("notifier: %v; reconnecting in 2s", err)
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

func (n *Notifier) listen(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, n.databaseURL)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	for _, ch := range n.channels {
		if _, err := conn.Exec(ctx, "listen "+ch); err != nil {
			return err
		}
	}
	log.Printf("notifier: listening on %v", n.channels)

	for {
		notif, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		n.broadcast(Notification{Channel: notif.Channel, Payload: notif.Payload})
	}
}

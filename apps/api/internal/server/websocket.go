package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type websocketBroadcaster struct {
	mu      sync.Mutex
	byFleet map[int64]map[*websocketConn]bool
}

type websocketConn struct {
	fleets map[int64]string
	send   chan []byte
}

func newWebsocketBroadcaster() *websocketBroadcaster {
	return &websocketBroadcaster{byFleet: map[int64]map[*websocketConn]bool{}}
}

func (f *websocketBroadcaster) add(c *websocketConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for fleet := range c.fleets {
		if f.byFleet[fleet] == nil {
			f.byFleet[fleet] = map[*websocketConn]bool{}
		}
		f.byFleet[fleet][c] = true
	}
}

func (f *websocketBroadcaster) remove(c *websocketConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for fleet := range c.fleets {
		if m := f.byFleet[fleet]; m != nil {
			delete(m, c)
			if len(m) == 0 {
				delete(f.byFleet, fleet)
			}
		}
	}
}

func (f *websocketBroadcaster) broadcast(fleet int64, kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.byFleet[fleet] {
		fleetID := c.fleets[fleet]
		if fleetID == "" {
			continue
		}
		msg, _ := json.Marshal(map[string]string{"t": kind, "fleet_id": fleetID})
		select {
		case c.send <- msg:
		default: // slow client: drop (the client resyncs on reconnect)
		}
	}
}

func (f *websocketBroadcaster) run(ctx context.Context, n *Notifier) {
	sub, unsubscribe := n.Subscribe(changedChannel)
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case note := <-sub:
			var p struct {
				T     string `json:"t"`
				Fleet int64  `json:"fleet"`
			}
			if json.Unmarshal([]byte(note.Payload), &p) != nil || p.Fleet == 0 {
				continue
			}
			f.broadcast(p.Fleet, p.T)
		}
	}
}

const (
	websocketWriteWait  = 10 * time.Second
	websocketPongWait   = 60 * time.Second
	websocketPingPeriod = 30 * time.Second
)

func (s *Server) websocketConnect(w http.ResponseWriter, r *http.Request) {
	fleets, err := s.q.ListFleetsForUser(r.Context(), currentUser(r).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	byFleet := make(map[int64]string, len(fleets))
	for _, fleet := range fleets {
		byFleet[fleet.ID] = uuidStr(fleet.PublicID)
	}
	// SameSite=Lax doesn't cover the WebSocket upgrade, so check Origin ourselves.
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return s.originAllowed(r, r.Header.Get("Origin")) },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &websocketConn{fleets: byFleet, send: make(chan []byte, 16)}
	s.websocketBroadcaster.add(c)

	go func() {
		ticker := time.NewTicker(websocketPingPeriod)
		defer func() { ticker.Stop(); conn.Close() }()
		for {
			select {
			case msg, ok := <-c.send:
				_ = conn.SetWriteDeadline(time.Now().Add(websocketWriteWait))
				if !ok {
					_ = conn.WriteMessage(websocket.CloseMessage, nil)
					return
				}
				if conn.WriteMessage(websocket.TextMessage, msg) != nil {
					return
				}
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(websocketWriteWait))
				if conn.WriteMessage(websocket.PingMessage, nil) != nil {
					return
				}
			}
		}
	}()

	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(websocketPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(websocketPongWait))
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	s.websocketBroadcaster.remove(c)
	close(c.send)
}

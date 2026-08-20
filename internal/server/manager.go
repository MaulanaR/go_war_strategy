package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MaulanaR/go_war_strategy/internal/game"
)

type roomMode string

const (
	modeSolo roomMode = "solo"
	modePvP  roomMode = "pvp"
)

type Manager struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewManager() *Manager {
	return &Manager{rooms: make(map[string]*Room)}
}

func (m *Manager) ServeWS(w http.ResponseWriter, r *http.Request) {
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	var room *Room

	switch mode {
	case "solo", "create":
		// The room is created only after a successful WebSocket upgrade so a
		// malformed request cannot leave an orphaned game loop behind.
	case "join":
		code := normalizeCode(r.URL.Query().Get("code"))
		room = m.findRoom(code)
		if room == nil {
			http.Error(w, "room tidak ditemukan", http.StatusNotFound)
			return
		}
	default:
		http.Error(w, "mode koneksi tidak valid", http.StatusBadRequest)
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "origin websocket tidak diizinkan", http.StatusForbidden)
		return
	}

	conn, err := upgradeWebSocket(w, r)
	if err != nil {
		return
	}

	if room == nil {
		roomMode := modeSolo
		if mode == "create" {
			roomMode = modePvP
		}
		room, err = m.createRoom(roomMode)
		if err != nil {
			_ = conn.WriteJSON(envelope{Type: "error", Message: err.Error()})
			_ = conn.Close()
			return
		}
	}

	client := newClient(conn, room)
	if err := room.addClient(client); err != nil {
		_ = conn.WriteJSON(envelope{Type: "error", Message: err.Error()})
		_ = conn.Close()
		room.closeIfEmpty()
		return
	}

	go client.writePump()
	client.readPump()
}

func (m *Manager) createRoom(mode roomMode) (*Room, error) {
	for attempts := 0; attempts < 20; attempts++ {
		code, err := randomCode(6)
		if err != nil {
			return nil, fmt.Errorf("generate room code: %w", err)
		}
		m.mu.Lock()
		if _, exists := m.rooms[code]; exists {
			m.mu.Unlock()
			continue
		}
		room := newRoom(m, code, mode)
		m.rooms[code] = room
		m.mu.Unlock()
		go room.run()
		return room, nil
	}
	return nil, errors.New("gagal membuat kode room unik")
}

func (m *Manager) findRoom(code string) *Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rooms[code]
}

func (m *Manager) removeRoom(code string, room *Room) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.rooms[code]; current == room {
		delete(m.rooms, code)
	}
}

type clientCommand struct {
	Type string        `json:"type"`
	Unit game.UnitType `json:"unit"`
}

type envelope struct {
	Type    string                          `json:"type"`
	Code    string                          `json:"code,omitempty"`
	Mode    roomMode                        `json:"mode,omitempty"`
	Side    game.Side                       `json:"side,omitempty"`
	Message string                          `json:"message,omitempty"`
	Catalog map[game.UnitType]game.UnitSpec `json:"catalog,omitempty"`
	State   *game.Snapshot                  `json:"state,omitempty"`
}

type Client struct {
	conn *wsConn
	room *Room
	side game.Side
	send chan []byte
	done chan struct{}
	once sync.Once
}

func newClient(conn *wsConn, room *Room) *Client {
	return &Client{
		conn: conn,
		room: room,
		send: make(chan []byte, 16),
		done: make(chan struct{}),
	}
}

func (c *Client) readPump() {
	defer c.close()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(45 * time.Second))

	for {
		var command clientCommand
		if err := c.conn.ReadJSON(&command); err != nil {
			return
		}
		switch command.Type {
		case "spawn":
			if err := c.room.spawn(c, command.Unit); err != nil {
				c.sendEnvelope(envelope{Type: "error", Message: err.Error()})
			}
		default:
			c.sendEnvelope(envelope{Type: "error", Message: "perintah tidak dikenal"})
		}
	}
}

func (c *Client) writePump() {
	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()
	defer c.close()

	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
			if err := c.conn.WriteMessage(wsTextMessage, message); err != nil {
				return
			}
		case <-pingTicker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
			if err := c.conn.WriteMessage(wsPingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) sendEnvelope(value envelope) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	select {
	case <-c.done:
		return
	case c.send <- payload:
	default:
		go c.close()
	}
}

func (c *Client) close() {
	c.once.Do(func() {
		c.room.removeClient(c)
		close(c.done)
		_ = c.conn.Close()
	})
}

type Room struct {
	manager *Manager
	code    string
	mode    roomMode
	mu      sync.Mutex
	game    *game.Game
	clients map[*Client]game.Side
	closed  bool
	started bool
	aiWait  float64
	rng     *mathrand.Rand
}

func newRoom(manager *Manager, code string, mode roomMode) *Room {
	match := game.New()
	if mode == modeSolo {
		match.Start()
	}
	return &Room{
		manager: manager,
		code:    code,
		mode:    mode,
		game:    match,
		clients: make(map[*Client]game.Side),
		aiWait:  0.9,
		rng:     mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
	}
}

func (r *Room) addClient(client *Client) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return errors.New("room sudah ditutup")
	}

	var side game.Side
	switch r.mode {
	case modeSolo:
		if len(r.clients) > 0 {
			return errors.New("sesi solo sudah digunakan")
		}
		side = game.Allies
	case modePvP:
		if len(r.clients) == 0 {
			side = game.Allies
		} else if len(r.clients) == 1 && !r.sideTaken(game.Axis) {
			side = game.Axis
		} else {
			return errors.New("room sudah penuh")
		}
	default:
		return errors.New("mode room tidak valid")
	}

	client.side = side
	r.clients[client] = side
	if r.mode == modePvP && len(r.clients) == 2 && !r.started {
		r.started = true
		r.game.Start()
	}

	welcome := envelope{
		Type: "welcome", Code: r.code, Mode: r.mode, Side: side, Catalog: game.Catalog,
	}
	client.sendEnvelope(welcome)
	state := r.game.Snapshot()
	client.sendEnvelope(envelope{Type: "state", State: &state})

	if r.mode == modePvP && len(r.clients) == 2 {
		r.broadcastLocked(envelope{Type: "notice", Message: "Lawan bergabung. Pertempuran dimulai!"})
	}
	return nil
}

func (r *Room) closeIfEmpty() {
	r.mu.Lock()
	if len(r.clients) != 0 || r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	r.manager.removeRoom(r.code, r)
}

func (r *Room) removeClient(client *Client) {
	r.mu.Lock()
	if _, exists := r.clients[client]; !exists {
		r.mu.Unlock()
		return
	}
	loser := r.clients[client]
	delete(r.clients, client)

	if r.mode == modePvP && r.started && r.game.Phase() == game.Playing {
		r.game.Forfeit(loser, "opponent disconnected")
	}
	shouldClose := len(r.clients) == 0
	if shouldClose {
		r.closed = true
	}
	r.mu.Unlock()

	if shouldClose {
		r.manager.removeRoom(r.code, r)
	}
}

func (r *Room) spawn(client *Client, unitType game.UnitType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	assigned, ok := r.clients[client]
	if !ok || assigned != client.side {
		return errors.New("player tidak terdaftar di room")
	}
	return r.game.Spawn(client.side, unitType)
}

func (r *Room) run() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	broadcastEvery := 0

	for range ticker.C {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return
		}
		if r.game.Phase() == game.Playing {
			if r.mode == modeSolo {
				r.stepAI(0.05)
			}
			r.game.Step(0.05)
		}
		broadcastEvery++
		if broadcastEvery >= 2 {
			broadcastEvery = 0
			state := r.game.Snapshot()
			r.broadcastLocked(envelope{Type: "state", State: &state})
		}
		r.mu.Unlock()
	}
}

func (r *Room) stepAI(dt float64) {
	r.aiWait -= dt
	if r.aiWait > 0 {
		return
	}

	state := r.game.Snapshot()
	resources := state.Players[game.Axis].Resources
	choices := []game.UnitType{game.Rifleman}
	if resources >= game.Catalog[game.Assault].Cost {
		choices = append(choices, game.Assault, game.Assault)
	}
	if resources >= game.Catalog[game.Gunner].Cost {
		choices = append(choices, game.Gunner)
	}
	if resources >= game.Catalog[game.Tank].Cost {
		choices = append(choices, game.Tank, game.Tank)
	}

	choice := choices[r.rng.Intn(len(choices))]
	if err := r.game.Spawn(game.Axis, choice); err != nil {
		r.aiWait = 0.35
		return
	}

	ramp := mathMin(0.5, state.Elapsed/240)
	r.aiWait = 0.72 + r.rng.Float64()*0.95 - ramp
}

func (r *Room) sideTaken(side game.Side) bool {
	for _, existing := range r.clients {
		if existing == side {
			return true
		}
	}
	return false
}

func (r *Room) broadcastLocked(value envelope) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	for client := range r.clients {
		select {
		case <-client.done:
		case client.send <- payload:
		default:
			go client.close()
		}
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}

func normalizeCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func randomCode(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		result[i] = alphabet[n.Int64()]
	}
	return string(result), nil
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a connected client
type Client struct {
	conn     *websocket.Conn
	username string
	send     chan Message
	room     string
	isHost   bool
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
	// Game state
	roundDurationSeconds int
	initialUSD           int
	rooms                map[string]*Room
	// mutex      sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:              make(map[*Client]bool),
		broadcast:            make(chan Message),
		register:             make(chan *Client),
		unregister:           make(chan *Client),
		roundDurationSeconds: 3,
		initialUSD:           1000,
		rooms:                make(map[string]*Room),
	}
}

// PlayerState holds per-player game info
type PlayerState struct {
	Username string `json:"username"`
	USD      int    `json:"usd"`
	Bribes   int    `json:"bribes"`
	Online   bool   `json:"online"`
}

// Game represents the ongoing game state
type Game struct {
	Players map[string]*PlayerState `json:"players"`
	Bets    map[string]int          `json:"-"`
	Round   int                     `json:"round"`
	Active  bool                    `json:"active"`
	// preserve join order
	PlayersOrder []string `json:"players_order"`
}

// Message represents messages exchanged over websocket
type Message struct {
	Username string          `json:"username"`
	Type     string          `json:"type"`
	Content  string          `json:"content"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Room     string          `json:"room,omitempty"`
}

// Room is a self-contained game room with its own clients and game
type Room struct {
	id                   string
	clients              map[*Client]bool
	register             chan *Client
	unregister           chan *Client
	broadcast            chan Message
	game                 *Game
	tick                 chan bool
	roundDurationSeconds int
	initialUSD           int
}

func newRoom(id string, roundSeconds, initial int) *Room {
	return &Room{
		id:                   id,
		clients:              make(map[*Client]bool),
		register:             make(chan *Client),
		unregister:           make(chan *Client),
		broadcast:            make(chan Message),
		tick:                 make(chan bool, 1),
		roundDurationSeconds: roundSeconds,
		initialUSD:           initial,
	}
}

func (r *Room) safeBroadcast(m Message) {
	select {
	case r.broadcast <- m:
	default:
	}
}

func (r *Room) initializeGame() {
	if r.game == nil {
		r.game = &Game{}
	}
	r.game.Players = make(map[string]*PlayerState)
	r.game.Bets = make(map[string]int)
	r.game.Round = 0
	r.game.Active = false
	r.game.PlayersOrder = []string{}

	for client := range r.clients {
		r.game.Players[client.username] = &PlayerState{Username: client.username, USD: r.initialUSD, Bribes: 0, Online: true}
		r.game.PlayersOrder = append(r.game.PlayersOrder, client.username)
	}
}

func (r *Room) startRound() {
	if r.game == nil {
		r.initializeGame()
	}
	if r.game.Active {
		return
	}
	r.game.Active = true
	r.game.Round++
	payload, _ := json.Marshal(map[string]interface{}{"round": r.game.Round, "duration": r.roundDurationSeconds})
	r.safeBroadcast(Message{Type: "round_start", Content: "Round started", Payload: payload, Room: r.id})
	go func() {
		timer := time.NewTimer(time.Duration(r.roundDurationSeconds) * time.Second)
		<-timer.C
		r.tick <- true
	}()
}

func (r *Room) resolveRound() {
	if r.game == nil || !r.game.Active {
		return
	}
	max := -1
	winners := []string{}
	for user, bet := range r.game.Bets {
		if bet > max {
			max = bet
			winners = []string{user}
		} else if bet == max {
			winners = append(winners, user)
		}
	}
	for user, bet := range r.game.Bets {
		if p, ok := r.game.Players[user]; ok {
			p.USD -= bet
			if p.USD < 0 {
				p.USD = 0
			}
		}
	}
	if max >= 0 {
		for _, w := range winners {
			if p, ok := r.game.Players[w]; ok {
				p.Bribes++
			}
		}
	}
	r.game.Bets = make(map[string]int)
	r.game.Active = false
	res := map[string]interface{}{"round": r.game.Round, "winners": winners, "max": max, "players": r.game.Players}
	payload, _ := json.Marshal(res)
	r.safeBroadcast(Message{Type: "round_result", Content: "Round resolved", Payload: payload, Room: r.id})
	alive := 0
	last := ""
	for _, p := range r.game.Players {
		if p.USD > 0 {
			alive++
			last = p.Username
		}
	}
	if alive <= 1 {
		if last != "" {
			if pl, ok := r.game.Players[last]; ok {
				pl.Bribes++
			}
		}
		final := map[string]interface{}{"players": r.game.Players}
		pay, _ := json.Marshal(final)
		r.safeBroadcast(Message{Type: "game_over", Content: "Game over", Payload: pay, Room: r.id})
	}
}

func (r *Room) run() {
	for {
		select {
		case client := <-r.register:
			r.clients[client] = true
			if r.game == nil {
				r.initializeGame()
			}
			if _, ok := r.game.Players[client.username]; !ok {
				r.game.Players[client.username] = &PlayerState{Username: client.username, USD: r.initialUSD, Bribes: 0, Online: true}
				r.game.PlayersOrder = append(r.game.PlayersOrder, client.username)
			} else {
				r.game.Players[client.username].Online = true
			}
			// notify room
			joinMsg := Message{Type: "join", Content: fmt.Sprintf("%s joined room %s", client.username, r.id), Room: r.id}
			r.safeBroadcast(joinMsg)
		case client := <-r.unregister:
			if _, ok := r.clients[client]; ok {
				delete(r.clients, client)
				close(client.send)
				if r.game != nil {
					if p, ok := r.game.Players[client.username]; ok {
						p.Online = false
					}
				}
				leaveMsg := Message{Type: "leave", Content: fmt.Sprintf("%s left room %s", client.username, r.id), Room: r.id}
				r.safeBroadcast(leaveMsg)
			}
		case <-r.tick:
			go r.resolveRound()
		case msg := <-r.broadcast:
			// route commands for this room
			switch msg.Type {
			case "bet":
				var p map[string]interface{}
				if len(msg.Payload) == 0 {
					// send to origin
					for c := range r.clients {
						if c.username == msg.Username {
							c.send <- Message{Type: "error", Content: "bet payload missing"}
						}
					}
					continue
				}
				_ = json.Unmarshal(msg.Payload, &p)
				v, ok := p["amount"]
				if !ok {
					for c := range r.clients {
						if c.username == msg.Username {
							c.send <- Message{Type: "error", Content: "bet payload missing 'amount'"}
						}
					}
					continue
				}
				amt := 0
				switch vt := v.(type) {
				case float64:
					amt = int(vt)
				case int:
					amt = vt
				}
				if r.game == nil {
					r.initializeGame()
				}
				if _, ok := r.game.Players[msg.Username]; !ok {
					r.game.Players[msg.Username] = &PlayerState{Username: msg.Username, USD: r.initialUSD, Bribes: 0, Online: true}
					r.game.PlayersOrder = append(r.game.PlayersOrder, msg.Username)
				}
				if !r.game.Active {
					for c := range r.clients {
						if c.username == msg.Username {
							c.send <- Message{Type: "error", Content: "no active round"}
						}
					}
					continue
				}
				if amt < 0 {
					amt = 0
				}
				if amt > r.game.Players[msg.Username].USD {
					amt = r.game.Players[msg.Username].USD
				}
				r.game.Bets[msg.Username] = amt
				conf, _ := json.Marshal(map[string]interface{}{"username": msg.Username, "amount": amt})
				r.safeBroadcast(Message{Type: "bet_confirm", Content: fmt.Sprintf("%s bet %d", msg.Username, amt), Payload: conf, Room: r.id})
				continue
			case "start":
				r.startRound()
				continue
			case "status":
				if r.game == nil {
					r.initializeGame()
				}
				state, _ := json.Marshal(r.game)
				r.safeBroadcast(Message{Type: "status", Content: "game state", Payload: state, Room: r.id})
				continue
			default:
			}
			// broadcast normal message to room clients
			for c := range r.clients {
				select {
				case c.send <- msg:
				default:
					close(c.send)
					delete(r.clients, c)
				}
			}
		}
	}
}

// generateRoomCode returns a code of length n using a friendly alphabet (no 0/O, 1/I, etc.)
func generateRoomCode(n int) (string, error) {
	alphabet := "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // 32 chars, avoids ambiguous ones
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		max := big.NewInt(int64(len(alphabet)))
		r, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[r.Int64()]
	}
	return string(out), nil
}

// Note: game lifecycle moved into Room; hub-level helpers removed.

// resolveRound determines winner(s) and updates game state
// hub-level round resolution removed; Room handles game lifecycle per-room.

func (h *Hub) run() {
	fmt.Println("Hub is running..!")
	for {
		select {
		case client := <-h.register:
			// ensure unique username across connected clients
			base := client.username
			suffix := 1
			unique := base
			for {
				conflict := false
				for c := range h.clients {
					if c.username == unique {
						conflict = true
						break
					}
				}
				if !conflict {
					break
				}
				suffix++
				unique = fmt.Sprintf("%s-%d", base, suffix)
			}
			if unique != client.username {
				client.username = unique
				// inform the client of their assigned name
				client.send <- Message{Type: "name_assigned", Content: client.username}
			}

			// keep global bookkeeping
			h.clients[client] = true

			// determine room
			roomID := client.room
			if roomID == "" {
				roomID = "lobby"
			}

			// create room if host and doesn't exist
			if _, ok := h.rooms[roomID]; !ok {
				if client.isHost {
					r := newRoom(roomID, h.roundDurationSeconds, h.initialUSD)
					h.rooms[roomID] = r
					go r.run()
				} else {
					// notify client that room doesn't exist
					client.send <- Message{Type: "error", Content: fmt.Sprintf("room %s does not exist", roomID)}
					continue
				}
			}

			// register client into the room
			h.rooms[roomID].register <- client
			fmt.Printf("Client %s connected to room %s. Total clients: %d\n", client.username, roomID, len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				roomID := client.room
				if roomID == "" {
					roomID = "lobby"
				}
				if r, ok := h.rooms[roomID]; ok {
					r.unregister <- client
				}

				log.Printf("Client %s disconnected. Total clients: %d", client.username, len(h.clients))
			}

		case message := <-h.broadcast:
			// include raw payload in debug log for easier diagnosis
			payloadStr := ""
			if len(message.Payload) > 0 {
				payloadStr = string(message.Payload)
			}
			log.Printf("hub received message: type=%s user=%s content=%s room=%s payload=%s\n", message.Type, message.Username, message.Content, message.Room, payloadStr)
			// Handle room creation/join requests which change client routing
			switch message.Type {
			case "create_room":
				// payload: {"room":"CODE"}
				var p map[string]interface{}
				if len(message.Payload) > 0 {
					_ = json.Unmarshal(message.Payload, &p)
				}
				roomID := ""
				if v, ok := p["room"].(string); ok {
					roomID = v
				}
				// find client pointer
				var cli *Client
				for c := range h.clients {
					if c.username == message.Username {
						cli = c
						break
					}
				}
				if cli == nil {
					continue
				}
				if roomID == "" {
					// generate a human-friendly short code
					for i := 0; i < 10; i++ {
						candidate, _ := generateRoomCode(4)
						if _, exists := h.rooms[candidate]; !exists {
							roomID = candidate
							break
						}
					}
					if roomID == "" {
						// fallback deterministic
						roomID = fmt.Sprintf("R%04d", time.Now().UnixNano()%10000)
					}
				}
				if _, ok := h.rooms[roomID]; !ok {
					r := newRoom(roomID, h.roundDurationSeconds, h.initialUSD)
					h.rooms[roomID] = r
					go r.run()
				}
				// move client to room
				cli.room = roomID
				h.rooms[roomID].register <- cli
				// reply with assigned room
				cli.send <- Message{Type: "room_created", Content: roomID}
				continue
			case "join_room":
				var p map[string]interface{}
				if len(message.Payload) > 0 {
					_ = json.Unmarshal(message.Payload, &p)
				}
				roomID := ""
				if v, ok := p["room"].(string); ok {
					roomID = v
				}
				var cli *Client
				for c := range h.clients {
					if c.username == message.Username {
						cli = c
						break
					}
				}
				if cli == nil {
					continue
				}
				if _, ok := h.rooms[roomID]; !ok {
					cli.send <- Message{Type: "error", Content: fmt.Sprintf("room %s not found", roomID)}
					continue
				}
				cli.room = roomID
				h.rooms[roomID].register <- cli
				cli.send <- Message{Type: "room_joined", Content: roomID}
				continue
			}
			// If the message targets a room, forward it to that room's broadcast channel
			if message.Room != "" {
				if r, ok := h.rooms[message.Room]; ok {
					// non-blocking forwarding into room
					select {
					case r.broadcast <- message:
					default:
					}
					continue
				}
			}

			// Global chat (not room-specific) - broadcast to all connected clients
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
}

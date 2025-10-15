package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a connected client
type Client struct {
	conn     *websocket.Conn
	username string
	send     chan Message
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
	// Game state
	game                 *Game
	tick                 chan bool
	roundDurationSeconds int
	initialUSD           int
	// mutex      sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		tick:                 make(chan bool, 1),
		roundDurationSeconds: 20,
		initialUSD:           1000,
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
	Round   int                    `json:"round"`
	Active  bool                   `json:"active"`
	// preserve join order
	PlayersOrder []string `json:"players_order"`
}

// Message represents messages exchanged over websocket
type Message struct {
	Username string          `json:"username"`
	Type     string          `json:"type"`
	Content  string          `json:"content"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// initializeGame prepares a new game
func (h *Hub) initializeGame() {
	if h.game == nil {
		h.game = &Game{}
	}
	h.game.Players = make(map[string]*PlayerState)
	h.game.Bets = make(map[string]int)
	h.game.Round = 0
	h.game.Active = false
	h.game.PlayersOrder = []string{}

	// Seed players from currently connected clients
	for client := range h.clients {
		h.game.Players[client.username] = &PlayerState{Username: client.username, USD: h.initialUSD, Bribes: 0, Online: true}
		h.game.PlayersOrder = append(h.game.PlayersOrder, client.username)
	}
}

// safeBroadcast sends a message into the broadcast channel without blocking if
// no goroutine is listening (useful for unit tests where hub.run may not be running).
func (h *Hub) safeBroadcast(m Message) {
	select {
	case h.broadcast <- m:
	default:
		// drop if nobody is listening
	}
}

// startRound starts a betting round with a timer
func (h *Hub) startRound() {
	if h.game == nil {
		h.initializeGame()
	}
	if h.game.Active {
		return
	}
	h.game.Active = true
	h.game.Round++

	// broadcast round start
	payload, _ := json.Marshal(map[string]interface{}{"round": h.game.Round, "duration": h.roundDurationSeconds})
	h.safeBroadcast(Message{Type: "round_start", Content: "Round started", Payload: payload})

	// start timer in a goroutine
	go func(roundAt int) {
		timer := time.NewTimer(time.Duration(h.roundDurationSeconds) * time.Second)
		<-timer.C
		// signal to hub.run to resolve
		h.tick <- true
		_ = roundAt
	}(h.game.Round)
}

// resolveRound determines winner(s) and updates game state
func (h *Hub) resolveRound() {
	if h.game == nil || !h.game.Active {
		return
	}

	// find max bet
	max := -1
	winners := []string{}
	for user, bet := range h.game.Bets {
		if bet > max {
			max = bet
			winners = []string{user}
		} else if bet == max {
			winners = append(winners, user)
		}
	}

	// everyone pays their wager
	for user, bet := range h.game.Bets {
		if p, ok := h.game.Players[user]; ok {
			p.USD -= bet
			if p.USD < 0 {
				p.USD = 0
			}
		}
	}

	// award bribes to winners
	if max >= 0 {
		for _, w := range winners {
			if p, ok := h.game.Players[w]; ok {
				p.Bribes++
			}
		}
	}

	// Clear bets
	h.game.Bets = make(map[string]int)
	h.game.Active = false

	// build result payload
	res := map[string]interface{}{
		"round":   h.game.Round,
		"winners": winners,
		"max":     max,
		"players": h.game.Players,
	}
	payload, _ := json.Marshal(res)
	h.safeBroadcast(Message{Type: "round_result", Content: "Round resolved", Payload: payload})

	// Check end condition: only one player with money or everyone out
	alive := 0
	last := ""
	for _, p := range h.game.Players {
		if p.USD > 0 {
			alive++
			last = p.Username
		}
	}
	if alive <= 1 {
		// give last person an automatic bribe if they have any money (rule)
		if last != "" {
			if pl, ok := h.game.Players[last]; ok {
				pl.Bribes++
			}
		}
		// game over
		final := map[string]interface{}{"players": h.game.Players}
	pay, _ := json.Marshal(final)
	h.safeBroadcast(Message{Type: "game_over", Content: "Game over", Payload: pay})
	}
}

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

			h.clients[client] = true

			// ensure game player exists and is marked online
			if h.game == nil {
				h.initializeGame()
			}
			if _, ok := h.game.Players[client.username]; !ok {
				h.game.Players[client.username] = &PlayerState{Username: client.username, USD: h.initialUSD, Bribes: 0, Online: true}
				h.game.PlayersOrder = append(h.game.PlayersOrder, client.username)
			} else {
				h.game.Players[client.username].Online = true
			}

			// Broadcast a join notification
			joinMsg := Message{Type: "join", Content: fmt.Sprintf("%s joined the game", client.username)}
			for c := range h.clients {
				select {
				case c.send <- joinMsg:
				default:
					close(c.send)
					delete(h.clients, c)
				}
			}

			fmt.Printf("Client %s connected. Total clients: %d\n", client.username, len(h.clients))

		case client := <-h.unregister:
			// h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// mark player offline but keep in Players map
				if h.game != nil {
					if p, ok := h.game.Players[client.username]; ok {
						p.Online = false
					}
				}

				// Broadcast a leave notification
				leaveMsg := Message{Type: "leave", Content: fmt.Sprintf("%s left the game", client.username)}
				for c := range h.clients {
					select {
					case c.send <- leaveMsg:
					default:
						close(c.send)
						delete(h.clients, c)
					}
				}

				log.Printf("Client %s disconnected. Total clients: %d", client.username, len(h.clients))
			}
			// h.mutex.Unlock()

		case <-h.tick:
			// resolve current round when timer fires
			go h.resolveRound()

		case message := <-h.broadcast:
			// Process server-side commands embedded in messages
			switch message.Type {
			case "bet":
				// payload expected: {"amount": <int>}
				var p map[string]interface{}
				if len(message.Payload) == 0 {
					// send error back to sender
					errMsg := Message{Type: "error", Content: "bet payload missing or malformed"}
					for c := range h.clients {
						if c.username == message.Username {
							c.send <- errMsg
							break
						}
					}
					continue
				}
				if err := json.Unmarshal(message.Payload, &p); err != nil {
					errMsg := Message{Type: "error", Content: "invalid bet payload"}
					for c := range h.clients {
						if c.username == message.Username {
							c.send <- errMsg
							break
						}
					}
					continue
				}
				v, ok := p["amount"]
				if !ok {
					errMsg := Message{Type: "error", Content: "bet payload missing 'amount'"}
					for c := range h.clients {
						if c.username == message.Username {
							c.send <- errMsg
							break
						}
					}
					continue
				}
				var amt int
				switch vt := v.(type) {
				case float64:
					amt = int(vt)
				case int:
					amt = vt
				default:
					errMsg := Message{Type: "error", Content: "bet 'amount' must be a number"}
					for c := range h.clients {
						if c.username == message.Username {
							c.send <- errMsg
							break
						}
					}
					continue
				}

				// ensure player exists
				if h.game == nil {
					h.initializeGame()
				}
				if _, ok := h.game.Players[message.Username]; !ok {
					// new player joins mid-game
					h.game.Players[message.Username] = &PlayerState{Username: message.Username, USD: h.initialUSD, Bribes: 0, Online: true}
					h.game.PlayersOrder = append(h.game.PlayersOrder, message.Username)
				}

				// validate active round
				if !h.game.Active {
					errMsg := Message{Type: "error", Content: "no active round; wait for a round to start"}
					for c := range h.clients {
						if c.username == message.Username {
							c.send <- errMsg
							break
						}
					}
					continue
				}

				// clamp bet
				if amt < 0 {
					amt = 0
				}
				if amt > h.game.Players[message.Username].USD {
					amt = h.game.Players[message.Username].USD
				}
				h.game.Bets[message.Username] = amt

				// broadcast confirmation
				conf, _ := json.Marshal(map[string]interface{}{"username": message.Username, "amount": amt})
				for client := range h.clients {
					select {
					case client.send <- Message{Type: "bet_confirm", Content: fmt.Sprintf("%s bet %d", message.Username, amt), Payload: conf}:
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
				continue
			case "start":
				// only start if not active
				h.startRound()
				continue
			case "status":
				// client requested game status; send current game state back
				if h.game == nil {
					h.initializeGame()
				}
				state, _ := json.Marshal(h.game)
				for client := range h.clients {
					select {
					case client.send <- Message{Type: "status", Content: "game state", Payload: state}:
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
				continue
			default:
				// fallthrough to normal broadcast for chat and other types
			}

			// Normal broadcast to clients
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

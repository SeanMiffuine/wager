package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	mrand "math/rand"
	"net/http"
	"strings"
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
		roundDurationSeconds: 30,
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
	Industry string `json:"industry"`
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
	Username  string          `json:"username"`
	Type      string          `json:"type"`
	Content   string          `json:"content"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Room      string          `json:"room,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// Room is a self-contained game room with its own clients and game
type Room struct {
	id                    string
	clients               map[*Client]bool
	register              chan *Client
	unregister            chan *Client
	broadcast             chan Message
	game                  *Game
	tick                  chan bool
	negotiation           bool
	finished              bool
	autoAdvance           bool
	roundDurationSeconds  int
	initialUSD            int
	interRoundSeconds     int
	host                  string
	currentSenatorName    string
	currentSenatorCountry string
}

func newRoom(id string, roundSeconds, initial int) *Room {
	return &Room{
		id:                    id,
		clients:               make(map[*Client]bool),
		register:              make(chan *Client),
		unregister:            make(chan *Client),
		broadcast:             make(chan Message),
		tick:                  make(chan bool, 1),
		negotiation:           false,
		finished:              false,
		autoAdvance:           false,
		roundDurationSeconds:  roundSeconds,
		initialUSD:            initial,
		interRoundSeconds:     7,
		currentSenatorName:    "",
		currentSenatorCountry: "",
	}
}

func (r *Room) safeBroadcast(m Message) {
	// Stamp message with server time if not already stamped.
	if m.Timestamp == "" {
		m.Timestamp = time.Now().Format("15:04:05")
	}
	// Deliver the message to all connected clients in this room without blocking.
	for c := range r.clients {
		select {
		case c.send <- m:
		default:
			// If the client's send buffer is full, drop the message for that client to avoid blocking.
		}
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
		r.game.Players[client.username] = &PlayerState{Username: client.username, USD: r.initialUSD, Bribes: 0, Online: true, Industry: randChoice(industries)}
		r.game.PlayersOrder = append(r.game.PlayersOrder, client.username)
	}
}

func (r *Room) startRound() {
	if r.game == nil {
		r.initializeGame()
	}
	// Do not start a new round if the room/game has finished
	if r.finished {
		return
	}
	if r.game.Active {
		return
	}
	r.game.Active = true
	r.game.Round++
	// reset bets for the new round
	r.game.Bets = make(map[string]int)
	// enter negotiation phase (chat allowed)
	r.negotiation = true
	// pick a silly senator for this round
	r.currentSenatorName = fmt.Sprintf("%s %s", randChoice(senatorFirst), randChoice(senatorLast))
	r.currentSenatorCountry = randChoice(countries)

	// if this is the first round, give a short story preface
	if r.game.Round == 1 {
		preface := strings.Join([]string{
			"==================================================",
			"",
			"WELCOME, CEOs of industry.",
			"You each start with $1000. Each round you will bribe a senator to gain influence.",
			"The player with the most bribes at the end becomes the Evil Corporate Supreme Leader.",
			"Negotiate with your rivals, then place your bribes when betting starts.",
			"",
			"==================================================",
		}, "\n")
		r.safeBroadcast(Message{Type: "message", Content: preface, Room: r.id})
	}

	payload, _ := json.Marshal(map[string]interface{}{"round": r.game.Round, "duration": r.roundDurationSeconds, "senator": r.currentSenatorName, "country": r.currentSenatorCountry})
	content := fmt.Sprintf("====\n\nROUND %d — Negotiation started\n\nYou are attempting to bribe %s from %s.\n\nDiscuss your strategy!\n\n====", r.game.Round, r.currentSenatorName, r.currentSenatorCountry)
	r.safeBroadcast(Message{Type: "round_start", Content: content, Payload: payload, Room: r.id})
	// start negotiation timer; when it expires, enter betting phase
	go func() {
		timer := time.NewTimer(time.Duration(r.roundDurationSeconds) * time.Second)
		<-timer.C
		r.negotiation = false
		// notify room that betting phase has started
		bp, _ := json.Marshal(map[string]interface{}{"round": r.game.Round})
		r.safeBroadcast(Message{Type: "betting_start", Content: "Betting started\n\nPlace your bets now!", Payload: bp, Room: r.id})
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
	// Determine if game reached terminal condition (only 0 or 1 players have money)
	alive := 0
	last := ""
	for _, p := range r.game.Players {
		if p.USD > 0 {
			alive++
			last = p.Username
		}
	}

	// If game is over, mark finished early to prevent races where a /start
	// could be processed between clearing active state and marking finished.
	if alive <= 1 {
		r.finished = true
		if last != "" {
			if pl, ok := r.game.Players[last]; ok {
				pl.Bribes++
				// announce winner publicly with a fun message (with spacing)
				winnerMsg := fmt.Sprintf("\n\n%s has won :) with %d bribes\n\n", pl.Username, pl.Bribes)
				r.safeBroadcast(Message{Type: "game_winner", Content: winnerMsg, Room: r.id})
			}
		}
	}

	// Build a detailed winners payload (username, amount they bet, current bribes)
	winnersInfo := []map[string]interface{}{}
	for _, w := range winners {
		betAmt := 0
		if b, ok := r.game.Bets[w]; ok {
			betAmt = b
		}
		br := 0
		if p, ok := r.game.Players[w]; ok {
			br = p.Bribes
		}
		winnersInfo = append(winnersInfo, map[string]interface{}{"username": w, "amount": betAmt, "bribes": br})
	}

	// Friendly, multi-line summary content for chat with clearer spacing
	var content string
	if len(winnersInfo) == 0 {
		content = fmt.Sprintf("\n\nROUND %d RESULT\n\nNo bets were placed this round.\n\n", r.game.Round)
	} else if len(winnersInfo) == 1 {
		wi := winnersInfo[0]
		uname := fmt.Sprintf("%v", wi["username"])
		amt := wi["amount"]
		br := wi["bribes"]
		// structured multi-line winner info
		content = fmt.Sprintf("\n\nROUND %d RESULT\n\n%s\nbribed $%v\ntotal bribes: %v\n\n", r.game.Round, uname, amt, br)
		// add a silly flourish referencing the senator on its own paragraph
		content = fmt.Sprintf("%s%s won round %d by taking %s out for a lavish dinner (and a whisper or two).\n\n", content, uname, r.game.Round, r.currentSenatorName)
	} else {
		// multiple winners: list each on its own block
		bparts := []string{}
		for _, wi := range winnersInfo {
			uname := fmt.Sprintf("%v", wi["username"])
			amt := wi["amount"]
			br := wi["bribes"]
			block := fmt.Sprintf("%s\nbribed $%v\ntotal bribes: %v\n", uname, amt, br)
			bparts = append(bparts, block)
		}
		content = fmt.Sprintf("\n\nROUND %d RESULT\n\n%s\n", r.game.Round, strings.Join(bparts, "\n"))
		content = fmt.Sprintf("%sThey charmed %s from %s with an unforgettable night out.\n\n", content, r.currentSenatorName, r.currentSenatorCountry)
	}

	// Announce winners publicly with amounts and updated bribe counts
	res := map[string]interface{}{"round": r.game.Round, "winners": winnersInfo}
	payload, _ := json.Marshal(res)
	r.safeBroadcast(Message{Type: "round_result", Content: content, Payload: payload, Room: r.id})

	// Add a clear separator so clients see a distinct break between the result
	// and the next round's start (helps readability in UIs/terminals).
	r.safeBroadcast(Message{Type: "message", Content: "\n\n======\n\n", Room: r.id})

	// If the room was marked finished above, broadcast final game over payload
	if r.finished {
		final := map[string]interface{}{"players": r.game.Players}
		pay, _ := json.Marshal(final)
		r.safeBroadcast(Message{Type: "game_over", Content: "\n\nGAME OVER\n\n", Payload: pay, Room: r.id})
	}

	// clear bets and mark round inactive
	r.game.Bets = make(map[string]int)
	r.game.Active = false

	// If the game is not finished and autoAdvance is enabled, start next round after a short buffer
	if !r.finished && r.autoAdvance {
		go func() {
			time.Sleep(time.Duration(r.interRoundSeconds) * time.Second)
			r.startRound()
		}()
	}

	// If the room was marked finished above, broadcast final game over payload
	if r.finished {
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
				r.game.Players[client.username] = &PlayerState{Username: client.username, USD: r.initialUSD, Bribes: 0, Online: true, Industry: randChoice(industries)}
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
				if !r.game.Active || r.negotiation {
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
				// Send bet confirmation privately to the bettor (do not reveal amount to others)
				conf, _ := json.Marshal(map[string]interface{}{"username": msg.Username, "amount": amt})
				for c := range r.clients {
					if c.username == msg.Username {
						c.send <- Message{Type: "bet_confirm", Content: "your bet accepted", Payload: conf, Room: r.id}
						break
					}
				}
				// Broadcast a generic notification that someone placed a bet (no amounts)
				r.safeBroadcast(Message{Type: "bet_placed", Content: fmt.Sprintf("%s has placed a bet", msg.Username), Room: r.id})
				// If all active players (USD>0 and online) have placed bets, resolve the round
				allPlaced := true
				for uname, pstate := range r.game.Players {
					if pstate.USD > 0 && pstate.Online {
						if _, ok := r.game.Bets[uname]; !ok {
							allPlaced = false
							break
						}
					}
				}
				if allPlaced {
					go r.resolveRound()
				}
				continue
			case "start":
				if r.finished {
					// cannot start when game finished
					for c := range r.clients {
						if c.username == msg.Username {
							c.send <- Message{Type: "error", Content: "game finished; use /restart to play again"}
							break
						}
					}
					continue
				}
				// Only allow explicit /start if the game hasn't begun yet. After the first start,
				// rounds advance automatically (autoAdvance true).
				if r.game == nil || r.game.Round == 0 {
					r.startRound()
					// enable automatic advancement of rounds
					r.autoAdvance = true
				} else {
					// game already started; inform originator that rounds auto-advance
					for c := range r.clients {
						if c.username == msg.Username {
							c.send <- Message{Type: "error", Content: "game already started; rounds advance automatically"}
							break
						}
					}
				}
				continue
			case "status":
				if r.game == nil {
					r.initializeGame()
				}
				state, _ := json.Marshal(r.game)
				r.safeBroadcast(Message{Type: "status", Content: "game state", Payload: state, Room: r.id})
				continue
			case "restart":
				// reset the room/game to initial state
				if r.game == nil {
					r.initializeGame()
				}
				// reset players USD and bribes, keep join order
				for _, p := range r.game.Players {
					p.USD = r.initialUSD
					p.Bribes = 0
					p.Online = true
				}
				r.game.Round = 0
				r.game.Active = false
				r.game.Bets = make(map[string]int)
				r.finished = false
				r.negotiation = false
				// notify room
				r.safeBroadcast(Message{Type: "room_restarted", Content: "Room has been restarted", Room: r.id})
				// also push a status update so clients (UIs) refresh their player tabs/lists
				state, _ := json.Marshal(r.game)
				r.safeBroadcast(Message{Type: "status", Content: "game state", Payload: state, Room: r.id})
				continue
			default:
			}
			// If this is a chat message and we're in the betting phase (negotiation ended), disallow chat
			if msg.Type == "message" && r.game != nil && r.game.Active && !r.negotiation {
				// send an error back to the originator only
				for c := range r.clients {
					if c.username == msg.Username {
						c.send <- Message{Type: "error", Content: "chat disabled during betting round"}
						break
					}
				}
				continue
			}

			// deliver message to room clients
			r.safeBroadcast(msg)
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

// Story data for industries and senators
var industries = []string{
	"English Textbooks", "Plastic Cups", "Luxury Toothpicks", "Synthetic Wool", "Streaming Ads", "Nanotech Balloons", "Quantum Toasters", "Corporate Espionage LLC", "Pasta Futures", "Disposable Drones",
	"Organic Ketchup", "Adult Coloring Books", "Foldable Sofas", "Biodegradable Glitter", "Electric Scooters", "AI-Powered Toasters", "Space Tourism Insurance", "Canned Sunshine", "Modular Umbrellas", "Scented USBs",
	"Cold Brew Tea Co.", "Augmented Reality Stickers", "Luxury Paperclips", "Waterproof Notebooks", "Pet Rocks Reimagined", "Solar-Powered Lampshades", "Hovering Planters", "Retro Polaroid Filters", "Miniature Wind Turbines", "Designer Bandages",
}

var senatorFirst = []string{
	"Bobby", "Sally", "Mortimer", "Trudy", "Hank", "Olive", "Baron", "Felicity", "Rex", "Gertie",
	"Percival", "Zelda", "Clarence", "Ingrid", "Maurice", "Daphne", "Lionel", "Prudence", "Quentin", "Margo",
	"Silas", "Lucinda", "Neville", "Beatrice", "Otis", "Wilhelmina", "Casper", "Marigold", "Tobias", "Eudora",
}

var senatorLast = []string{
	"Smith", "O'Leary", "Zamboni", "Quincy", "Mcdowell", "Nakamoto", "VonBleu", "Smirk", "Bumble", "Hargrove",
	"Featherstone", "Blackwell", "Kingman", "Alder", "Hooten", "Bramble", "Nightshade", "Clearwater", "Pine", "Silverton",
	"Crowley", "Fairbanks", "Thornberry", "Galloway", "Pembroke", "Wainscott", "Redford", "Thistle", "Longbottom", "Huxley",
}

var countries = []string{
	"Zimbabwe", "Norway", "Tonga", "Botswana", "France", "Narnia", "Poland", "Peru", "United States", "Atlantis",
	"Wakanda", "Mongolia", "Iceland", "Chile", "Madagascar", "Luxembourg", "Brazil", "Canada", "Japan", "Samoa",
	"Seychelles", "Romania", "Czechia", "Germany", "Australia", "India", "Mexico", "Egypt", "Portugal", "Bahrain",
}

var rng = mrand.New(mrand.NewSource(time.Now().UnixNano()))

func randChoice(arr []string) string {
	if len(arr) == 0 {
		return ""
	}
	return arr[rng.Intn(len(arr))]
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
					r.host = client.username
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
					r.host = cli.username
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
			case "users":
				// Return a list of users (room-scoped if provided)
				var p map[string]interface{}
				if len(message.Payload) > 0 {
					_ = json.Unmarshal(message.Payload, &p)
				}
				// find client
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
				roomID := ""
				if v, ok := p["room"].(string); ok && v != "" {
					roomID = v
				} else {
					roomID = cli.room
				}
				// global list if no room
				if roomID == "" {
					names := []string{}
					for c := range h.clients {
						names = append(names, c.username)
					}
					b, _ := json.Marshal(names)
					cli.send <- Message{Type: "users", Content: "users_list", Payload: b}
					continue
				}
				r, ok := h.rooms[roomID]
				if !ok {
					cli.send <- Message{Type: "error", Content: fmt.Sprintf("room %s not found", roomID)}
					continue
				}
				names := []string{}
				for c := range r.clients {
					names = append(names, c.username)
				}
				b, _ := json.Marshal(names)
				cli.send <- Message{Type: "users", Content: "users_list", Payload: b}
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

			// If this is a plain chat message without a target room, do not broadcast globally.
			// Chat should be room-scoped only. Other non-message types are still broadcast globally.
			if message.Type == "message" && message.Room == "" {
				// ignore plain global chat messages (pre-room)
				continue
			}

			// Global broadcast for non-chat messages (or others if needed)
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

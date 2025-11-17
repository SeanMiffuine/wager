package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func handleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	fmt.Println("WebSocket connection established")

	// Get username from query parameter
	username := r.URL.Query().Get("username")
	if username == "" {
		username = "Anonymous"
	}

	fmt.Println("Username parsed:", username)

	client := &Client{
		conn:     conn,
		username: username,
		send:     make(chan Message, 256),
	}

	// something here doesnt go through after 1 connection

	hub.register <- client

	fmt.Println("New client connected:", username)
	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump(hub)
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.conn.Close()
	}()

	for {
		var msg Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Debug: log the raw message received from client
		payloadStr := ""
		if len(msg.Payload) > 0 {
			payloadStr = string(msg.Payload)
		}
		log.Printf("readPump recv: type=%s user_payload=%s content=%s room=%s\n", msg.Type, payloadStr, msg.Content, msg.Room)

		// Preserve message type if client provided it; otherwise default to "message"
		msg.Username = c.username
		if msg.Type == "" {
			msg.Type = "message"
		}

		hub.broadcast <- msg
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteJSON(message); err != nil {
				log.Println("WebSocket write error:", err)
				return
			}
		}
	}
}

func main() {

	var serverMode = flag.Bool("s", false, "Run as server")
	var localMode = flag.Bool("l", false, "Run server locally")
	var serverAddr string
	var scheme string

	flag.Parse() // parse flags
	reader := bufio.NewReader(os.Stdin)

	if *localMode { // after parse
		serverAddr = "localhost:8080"
		scheme = "ws"
	} else {
		serverAddr = "wager-kd8l.onrender.com"
		scheme = "wss"
	}

	if *serverMode {
		// if setup server
		hub := newHub()
		go hub.run()

		http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			handleWebSocket(hub, w, r)
		})

		// Get and display server information
		fmt.Println("=== WebSocket Chat Server ===")
		fmt.Println("Share this information with clients:")

		if *localMode {
			serverAddr = "localhost:8080"
			fmt.Println("Running locally on ws://localhost:8080/ws")
			fmt.Println("=============================")
			log.Fatal(http.ListenAndServe(serverAddr, nil))
		} else {
			port := os.Getenv("PORT")
			fmt.Println("Serving on Render's PORT env variable")
			fmt.Println("=============================")
			log.Fatal(http.ListenAndServe(":"+port, nil))
		}
		// server ends here
	}

	fmt.Print("Enter your username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	if username == "" {
		username = "Anonymous"
	}

	// Create WebSocket URL
	u := url.URL{
		Scheme:   scheme,
		Host:     serverAddr,
		Path:     "/ws",
		RawQuery: "username=" + url.QueryEscape(username),
	}

	fmt.Printf("Connecting to %s as %s...\n", u.String(), username)

	// Connect to WebSocket server
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("Failed to connect:", err)
	}
	defer conn.Close()

	fmt.Println("Connected! Type messages and press Enter to send.")
	fmt.Println("Type 'quit' to exit. Commands: /create [CODE], /join CODE, /start, /bet AMOUNT, /status, /users, /help")
	fmt.Println("========================================")

	// Channel to handle interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Channel for sending messages
	sendChan := make(chan string)

	// Goroutine to handle incoming messages
	currentRoom := ""
	go func() {
		for {
			var msg Message
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Println("Read error:", err)
				return
			}

			// Format and display the message based on type. Prefer server-provided timestamp when present.
			timestamp := ""
			if msg.Timestamp != "" {
				timestamp = msg.Timestamp
			} else {
				timestamp = time.Now().Format("15:04:05")
			}
			switch msg.Type {
			case "message":
				fmt.Printf("[%s] %s: %s\n", timestamp, msg.Username, msg.Content)
			case "join":
				fmt.Printf("[%s] *** %s ***\n", timestamp, msg.Content)
			case "leave":
				fmt.Printf("[%s] *** %s ***\n", timestamp, msg.Content)
			case "room_created":
				// server informs client of created room
				fmt.Printf("[%s] Room created: %s\n", timestamp, msg.Content)
				currentRoom = msg.Content
			case "room_joined":
				fmt.Printf("[%s] Joined room: %s\n", timestamp, msg.Content)
				currentRoom = msg.Content
			case "bet_confirm":
				fmt.Printf("[%s] %s\n", timestamp, msg.Content)
			case "round_start":
				fmt.Printf("[%s] ROUND START: %s\n", timestamp, msg.Content)
				if len(msg.Payload) > 0 {
					var p map[string]interface{}
					_ = json.Unmarshal(msg.Payload, &p)
					if r, ok := p["round"]; ok {
						fmt.Printf("    round: %v\n", r)
					}
				}
			case "round_result":
				fmt.Printf("[%s] ROUND RESULT: %s\n", timestamp, msg.Content)
				if len(msg.Payload) > 0 {
					var p map[string]interface{}
					_ = json.Unmarshal(msg.Payload, &p)
					b, _ := json.MarshalIndent(p, "    ", "  ")
					fmt.Println(string(b))
				}
			case "status":
				fmt.Printf("[%s] STATUS: %s\n", timestamp, msg.Content)
				if len(msg.Payload) > 0 {
					var p map[string]interface{}
					_ = json.Unmarshal(msg.Payload, &p)
					b, _ := json.MarshalIndent(p, "    ", "  ")
					fmt.Println(string(b))
				}
			case "game_over":
				fmt.Printf("[%s] GAME OVER: %s\n", timestamp, msg.Content)
				if len(msg.Payload) > 0 {
					var p map[string]interface{}
					_ = json.Unmarshal(msg.Payload, &p)
					b, _ := json.MarshalIndent(p, "    ", "  ")
					fmt.Println(string(b))
				}
			case "error":
				fmt.Printf("[%s] ERROR: %s\n", timestamp, msg.Content)
			case "name_assigned":
				fmt.Printf("[%s] Name assigned: %s\n", timestamp, msg.Content)
			case "users":
				// payload expected to be a JSON array
				if len(msg.Payload) > 0 {
					var arr []string
					_ = json.Unmarshal(msg.Payload, &arr)
					fmt.Printf("[%s] Users: %v\n", timestamp, arr)
				} else {
					fmt.Printf("[%s] Users: %s\n", timestamp, msg.Content)
				}
			default:
				// unknown type - print raw
				fmt.Printf("[%s] %s: %s (type=%s)\n", timestamp, msg.Username, msg.Content, msg.Type)
			}
		}
	}()

	// Goroutine to handle user input
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "quit" {
				interrupt <- os.Interrupt
				return
			}
			if text != "" {
				sendChan <- text
			}
		}
	}()

	// Main loop
	for {
		select {
		case message := <-sendChan:
			// Handle special commands
			if strings.HasPrefix(message, "/") {
				parts := strings.Fields(message)
				cmd := parts[0]
				switch cmd {
				case "/create":
					// optional code
					payload := map[string]string{}
					if len(parts) > 1 {
						payload["room"] = parts[1]
					}
					b, _ := json.Marshal(payload)
					msg := Message{Username: username, Type: "create_room", Payload: b}
					_ = conn.WriteJSON(msg)
					continue
				case "/join":
					if len(parts) < 2 {
						fmt.Println("Usage: /join CODE")
						continue
					}
					payload := map[string]string{"room": parts[1]}
					b, _ := json.Marshal(payload)
					msg := Message{Username: username, Type: "join_room", Payload: b}
					_ = conn.WriteJSON(msg)
					continue
				case "/start":
					if currentRoom == "" {
						fmt.Println("You are not in a room. Join or create one first.")
						continue
					}
					msg := Message{Username: username, Type: "start", Room: currentRoom}
					_ = conn.WriteJSON(msg)
					continue
				case "/bet":
					if currentRoom == "" {
						fmt.Println("You are not in a room. Join or create one first.")
						continue
					}
					if len(parts) < 2 {
						fmt.Println("Usage: /bet AMOUNT")
						continue
					}
					amt := 0
					fmt.Sscanf(parts[1], "%d", &amt)
					payload := map[string]int{"amount": amt}
					b, _ := json.Marshal(payload)
					msg := Message{Username: username, Type: "bet", Room: currentRoom, Payload: b}
					_ = conn.WriteJSON(msg)
					continue
				case "/status":
					if currentRoom == "" {
						fmt.Println("You are not in a room. Join or create one first.")
						continue
					}
					msg := Message{Username: username, Type: "status", Room: currentRoom}
					_ = conn.WriteJSON(msg)
					continue
				case "/restart":
					if currentRoom == "" {
						fmt.Println("You are not in a room. Join or create one first.")
						continue
					}
					msg := Message{Username: username, Type: "restart", Room: currentRoom}
					_ = conn.WriteJSON(msg)
					continue
				case "/users":
					// request users in current room (or global if not in room)
					payload := map[string]string{}
					if currentRoom != "" {
						payload["room"] = currentRoom
					}
					b, _ := json.Marshal(payload)
					msg := Message{Username: username, Type: "users", Payload: b}
					_ = conn.WriteJSON(msg)
					continue
				case "/help":
					fmt.Println("Available commands: /create [CODE], /join CODE, /start, /bet AMOUNT, /status, /users, /help, quit")
					continue
				default:
					fmt.Println("Unknown command. Type /help for available commands")
					continue
				}
			}

			msg := Message{
				Username: username,
				Content:  message,
				Type:     "message",
				Room:     currentRoom,
			}

			err := conn.WriteJSON(msg)
			if err != nil {
				log.Println("Write error:", err)
				return
			}
			// fmt.Printf("[%s] You: %s\n", time.Now().Format("15:04:05"), message)

		case <-interrupt:
			fmt.Println("\nDisconnecting...")

			// Send close message
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("Write close error:", err)
				return
			}

			// Wait for server to close connection
			select {
			case <-time.After(time.Second):
			}
			return
		}
	}
}

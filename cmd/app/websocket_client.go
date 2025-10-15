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

// Message type is declared in websocket_server.go and shared across the package.

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

		// Set the username from the client
		msg.Username = c.username

		// forward message to hub for processing (chat/bet/start/status)
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
	var roundSeconds = flag.Int("round", 20, "Round duration in seconds")
	var initialUSD = flag.Int("initial", 1000, "Initial USD for each player")
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
	// apply configurable options
	hub.roundDurationSeconds = *roundSeconds
	hub.initialUSD = *initialUSD
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
	fmt.Println("Type 'quit' to exit, '/users' to see online users.")
	fmt.Println("========================================")

	// Channel to handle interrupt signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Channel for sending messages
	sendChan := make(chan string)

	// Goroutine to handle incoming messages
	go func() {
		for {
			var msg Message
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Println("Read error:", err)
				return
			}

				// Format and display the message based on type
				timestamp := time.Now().Format("15:04:05")
				switch msg.Type {
				case "message":
					fmt.Printf("[%s] %s: %s\n", timestamp, msg.Username, msg.Content)
				case "join":
					fmt.Printf("[%s] *** %s ***\n", timestamp, msg.Content)
				case "name_assigned":
					// server assigned or adjusted our username
					fmt.Printf("[%s] *** Assigned username: %s ***\n", timestamp, msg.Content)
				case "leave":
					fmt.Printf("[%s] *** %s ***\n", timestamp, msg.Content)
				case "round_start":
					var p map[string]interface{}
					if len(msg.Payload) > 0 {
						_ = json.Unmarshal(msg.Payload, &p)
					}
					fmt.Printf("[%s] *** Round %v started (duration %vs) ***\n", timestamp, p["round"], p["duration"])
				case "round_result":
					var res map[string]interface{}
					if len(msg.Payload) > 0 {
						_ = json.Unmarshal(msg.Payload, &res)
					}
					fmt.Printf("[%s] *** Round %v resolved. Winners: %v, Max: %v ***\n", timestamp, res["round"], res["winners"], res["max"])
				case "game_over":
					var final map[string]interface{}
					if len(msg.Payload) > 0 {
						_ = json.Unmarshal(msg.Payload, &final)
					}
					fmt.Printf("[%s] *** GAME OVER ***\nPlayers:\n%v\n", timestamp, final["players"])
				default:
					// unknown type - print raw
					fmt.Printf("[%s] %s: %s\n", timestamp, msg.Username, msg.Content)
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
				parts := strings.SplitN(message, " ", 2)
				cmd := parts[0]
				switch cmd {
				case "/bet":
					amt := 0
					if len(parts) > 1 {
						fmt.Sscanf(parts[1], "%d", &amt)
					} else {
						fmt.Println("Usage: /bet <amount>")
						continue
					}
					payload, _ := json.Marshal(map[string]int{"amount": amt})
					msg := Message{Username: username, Type: "bet", Content: fmt.Sprintf("bet %d", amt), Payload: payload}
					if err := conn.WriteJSON(msg); err != nil {
						log.Println("Write error:", err)
						return
					}
					continue
				case "/start":
					msg := Message{Username: username, Type: "start", Content: "start"}
					if err := conn.WriteJSON(msg); err != nil {
						log.Println("Write error:", err)
						return
					}
					continue
				case "/status":
					msg := Message{Username: username, Type: "status", Content: "status"}
					if err := conn.WriteJSON(msg); err != nil {
						log.Println("Write error:", err)
						return
					}
					continue
				case "/help":
					fmt.Println("*** Available commands: /bet <amount>, /start, /status, /help, quit ***")
					continue
				default:
					fmt.Println("*** Unknown command. Type /help for available commands ***")
					continue
				}
			}

			msg := Message{
				Username: username,
				Content:  message,
				Type:     "message",
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

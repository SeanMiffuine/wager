package main

import (
	// "encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// Client represents a connected client
type Client struct {
	conn     *websocket.Conn
	username string
	send     chan Message
	id       int // identify which client
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	usernames  map[string]bool // needed to prevent dupes
	clients    map[*Client]bool
	broadcast  chan Message
	register   chan *Client
	unregister chan *Client
	// mutex      sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	fmt.Println("Hub is running..!")
	for {
		select {
		case client := <-h.register:

			h.clients[client] = true

			fmt.Printf("Client %s connected. Total clients: %d\n", client.username, len(h.clients))

		case client := <-h.unregister:
			// h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// // Notify everyone that someone left
				// leaveMsg := Message{
				// 	Username: client.username,
				// 	Content:  fmt.Sprintf("%s left the chat", client.username),
				// 	Type:     "leave",
				// }
				// h.broadcast <- leaveMsg

				log.Printf("Client %s disconnected. Total clients: %d", client.username, len(h.clients))
			}
			// h.mutex.Unlock()

		case message := <-h.broadcast:
			// h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			// h.mutex.RUnlock()
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
}

package main

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

// on server, keep some sort of enum for all commands, and whether or not there are arguements ???

// Calling server to create a game room, returns a room code to join
func createRoom(conn *websocket.Conn) {
	err := conn.WriteJSON(Message{
		Type: "create_room",
	})
	if err != nil {
		log.Println("Error creating room:", err)
		return
	}
}

// Join a game room with a given code
func joinRoom(conn *websocket.Conn, roomCode string) {
	err := conn.WriteJSON(Message{
		Type:    "join_room",
		Content: roomCode,
	})
	if err != nil {
		log.Println("Error joining room:", err)
		return
	}
}

// Click to start game
func startGame(conn *websocket.Conn, roomCode string) {
	err := conn.WriteJSON(Message{
		Type:    "start_game",
		Content: roomCode,
	})
	if err != nil {
		log.Println("Error starting game:", err)
		return
	}
}

// Wager action in game
func wager(conn *websocket.Conn, roomCode string, action string, amount int) {
	err := conn.WriteJSON(Message{
		Type:    "wager",
		Content: fmt.Sprintf("%s %d", action, amount),
	})
	if err != nil {
		log.Println("Error sending wager action:", err)
		return
	}
}

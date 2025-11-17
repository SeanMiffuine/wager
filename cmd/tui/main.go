package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/gorilla/websocket"
	"github.com/rivo/tview"
)

type Message struct {
	Username  string          `json:"username"`
	Type      string          `json:"type"`
	Content   string          `json:"content"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Room      string          `json:"room,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

func main() {
	server := flag.String("server", "localhost:8080", "websocket server host:port")
	scheme := flag.String("scheme", "ws", "websocket scheme: ws or wss")
	username := flag.String("u", "guest", "username")
	flag.Parse()

	app := tview.NewApplication()

	chat := tview.NewTextView().
		SetDynamicColors(true).
		SetChangedFunc(func() { app.Draw() })
	chat.SetBorder(true).SetTitle("Chat")

	players := tview.NewTextView().SetDynamicColors(true)
	players.SetBorder(true).SetTitle("Players")
	players.SetWrap(true)
	players.SetWordWrap(true)
	players.SetChangedFunc(func() { app.Draw() })

	input := tview.NewInputField().SetLabel("Message: ")
	input.SetFieldWidth(0)
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
	})

	// layout
	mainFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	mainFlex.AddItem(chat, 0, 3, false)
	mainFlex.AddItem(players, 30, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	root.AddItem(mainFlex, 0, 1, true)
	root.AddItem(input, 3, 0, true)

	// websocket connection
	u := url.URL{Scheme: *scheme, Host: *server, Path: "/ws", RawQuery: "username=" + url.QueryEscape(*username)}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("failed to connect to %s: %v", u.String(), err)
	}

	sendCh := make(chan Message, 16)
	quit := make(chan struct{})

	var currentRoom string

	// writer
	go func() {
		for {
			select {
			case m := <-sendCh:
				_ = conn.WriteJSON(m)
			case <-quit:
				return
			}
		}
	}()

	// helper to append to chat view
	appendChat := func(line string) {
		fmt.Fprintln(chat, line)
		// keep view pinned to the end so new messages are visible
		chat.ScrollToEnd()
	}

	// reader
	go func() {
		defer close(quit)
		for {
			var m Message
			if err := conn.ReadJSON(&m); err != nil {
				app.QueueUpdateDraw(func() {
					appendChat(fmt.Sprintf("[red]Connection closed: %v", err))
				})
				return
			}
			// handle messages
			app.QueueUpdateDraw(func() {
				ts := m.Timestamp
				if ts == "" {
					ts = "--:--:--"
				}
				switch m.Type {
				case "message":
					appendChat(fmt.Sprintf("[%s] %s: %s", ts, m.Username, m.Content))
				case "join", "leave":
					appendChat(fmt.Sprintf("[%s] *** %s ***", ts, m.Content))
				case "room_created":
					currentRoom = m.Content
					appendChat(fmt.Sprintf("[%s] Room created: %s", ts, m.Content))
				case "room_joined":
					currentRoom = m.Content
					appendChat(fmt.Sprintf("[%s] Joined room: %s", ts, m.Content))
				case "bet_confirm":
					appendChat(fmt.Sprintf("[%s] (private) %s", ts, m.Content))
				case "round_start":
					appendChat(fmt.Sprintf("[%s] ROUND START: %s", ts, m.Content))
				case "betting_start":
					appendChat(fmt.Sprintf("[%s] BETTING START: %s", ts, m.Content))
				case "round_result":
					appendChat(fmt.Sprintf("[%s] ROUND RESULT: %s", ts, m.Content))
				case "status":
					// Update players panel if payload present. Do not append a chat message
					// for status responses — they should silently refresh the player list.
					if len(m.Payload) > 0 {
						var st map[string]interface{}
						if err := json.Unmarshal(m.Payload, &st); err == nil {
							if playersObj, ok := st["players"].(map[string]interface{}); ok {
								// Render players into the TextView. Prefer `players_order` if present.
								var b strings.Builder
								// Try to respect players_order ordering
								if orderRaw, ok := st["players_order"]; ok {
									if orderArr, ok := orderRaw.([]interface{}); ok {
										for _, v := range orderArr {
											if uname, ok := v.(string); ok {
												if raw, ok := playersObj[uname]; ok {
													if pmap, ok := raw.(map[string]interface{}); ok {
														usd := 0
														br := 0
														online := true
														industry := ""
														if vv, ok := pmap["usd"].(float64); ok {
															usd = int(vv)
														}
														if vv, ok := pmap["bribes"].(float64); ok {
															br = int(vv)
														}
														if vv, ok := pmap["online"].(bool); ok {
															online = vv
														}
														if vv, ok := pmap["industry"].(string); ok {
															industry = vv
														}
														label := fmt.Sprintf("CEO of %s: %s  —  $%d  bribes:%d", industry, uname, usd, br)
														if !online {
															label = label + " (offline)"
														}
														b.WriteString(label)
														b.WriteString("\n\n")
													}
												}
											}
										}
									}
								}
								// Add any remaining players not listed in players_order
								for uname, raw := range playersObj {
									if strings.Contains(b.String(), uname) {
										continue
									}
									if pmap, ok := raw.(map[string]interface{}); ok {
										usd := 0
										br := 0
										online := true
										industry := ""
										if v, ok := pmap["usd"].(float64); ok {
											usd = int(v)
										}
										if v, ok := pmap["bribes"].(float64); ok {
											br = int(v)
										}
										if v, ok := pmap["online"].(bool); ok {
											online = v
										}
										if v, ok := pmap["industry"].(string); ok {
											industry = v
										}
										label := fmt.Sprintf("CEO of %s: %s  —  $%d  bribes:%d", industry, uname, usd, br)
										if !online {
											label = label + " (offline)"
										}
										b.WriteString(label)
										b.WriteString("\n")
									}
								}
								players.SetText(b.String())
							}
						}
					}
				case "users":
					if len(m.Payload) > 0 {
						var arr []string
						if err := json.Unmarshal(m.Payload, &arr); err == nil {
							var b strings.Builder
							for _, n := range arr {
								b.WriteString(n)
								b.WriteString("\n")
							}
							players.SetText(b.String())
						}
					}
				case "error":
					appendChat(fmt.Sprintf("[red] ERROR: %s", m.Content))
				default:
					appendChat(fmt.Sprintf("[%s] %s: %s (type=%s)", ts, m.Username, m.Content, m.Type))
				}

				// For room-changing events, request a fresh status so player list updates
				switch m.Type {
				case "room_created", "room_joined", "join", "leave", "round_start", "betting_start", "round_result", "game_over":
					// determine which room to query; prefer message Room, fallback to currentRoom
					roomToQuery := m.Room
					if roomToQuery == "" {
						roomToQuery = currentRoom
					}
					if roomToQuery != "" {
						// send a status request to refresh players list
						sendCh <- Message{Username: *username, Type: "status", Room: roomToQuery}
					}
				}
			})
		}
	}()

	// Input handling: commands or chat
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		text := strings.TrimSpace(input.GetText())
		input.SetText("")
		if text == "" {
			return
		}
		if strings.HasPrefix(text, "/") {
			parts := strings.Fields(text)
			cmd := parts[0]
			switch cmd {
			case "/create":
				payload := map[string]string{}
				if len(parts) > 1 {
					payload["room"] = parts[1]
				}
				b, _ := json.Marshal(payload)
				sendCh <- Message{Username: *username, Type: "create_room", Payload: b}
			case "/join":
				if len(parts) < 2 {
					appendChat("Usage: /join CODE")
					return
				}
				payload := map[string]string{"room": parts[1]}
				b, _ := json.Marshal(payload)
				sendCh <- Message{Username: *username, Type: "join_room", Payload: b}
			case "/start":
				if currentRoom == "" {
					appendChat("Not in a room")
					return
				}
				sendCh <- Message{Username: *username, Type: "start", Room: currentRoom}
			case "/bet":
				if currentRoom == "" {
					appendChat("Not in a room")
					return
				}
				if len(parts) < 2 {
					appendChat("Usage: /bet AMOUNT")
					return
				}
				amt := 0
				fmt.Sscanf(parts[1], "%d", &amt)
				payload := map[string]int{"amount": amt}
				b, _ := json.Marshal(payload)
				sendCh <- Message{Username: *username, Type: "bet", Room: currentRoom, Payload: b}
			case "/status":
				if currentRoom == "" {
					appendChat("Not in a room")
					return
				}
				sendCh <- Message{Username: *username, Type: "status", Room: currentRoom}
			case "/users":
				payload := map[string]string{}
				if currentRoom != "" {
					payload["room"] = currentRoom
				}
				b, _ := json.Marshal(payload)
				sendCh <- Message{Username: *username, Type: "users", Payload: b}
			case "/restart":
				if currentRoom == "" {
					appendChat("Not in a room")
					return
				}
				sendCh <- Message{Username: *username, Type: "restart", Room: currentRoom}
			case "/help":
				appendChat("Commands: /create [CODE], /join CODE, /start, /bet AMOUNT, /status, /users, /restart, /help")
			default:
				appendChat("Unknown command. Type /help")
			}
			return
		}
		// plain chat
		sendCh <- Message{Username: *username, Type: "message", Room: currentRoom, Content: text}
	})

	if err := app.SetRoot(root, true).EnableMouse(true).Run(); err != nil {
		log.Fatalf("error running app: %v", err)
	}
}

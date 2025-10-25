package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"

	"github.com/gdamore/tcell/v2"
)

const (
	TERMINAL_WIDTH  = 162
	TERMINAL_HEIGHT = 42
)

// Orchestrator for the tcell mockup. Wires modules and re-renders on updates.
func main() {
	// If the wrapper script already did a resize and exec'd us, it sets
	// WAGER_RESIZE_DONE=1 in the environment. In that case skip the extra
	if os.Getenv("WAGER_RESIZE_DONE") == "" {
		tryRunResize(TERMINAL_HEIGHT, TERMINAL_WIDTH)
	} else {
		log.Printf("WAGER_RESIZE_DONE=1 detected; skipping internal resize attempt")
	}

	s, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("failed to create screen: %v", err)
	}
	if err := s.Init(); err != nil {
		log.Fatalf("failed to init screen: %v", err)
	}
	// diagnostic: report terminal color capabilities and relevant env
	log.Printf("TERM=%s COLORTERM=%s TCELL_TRUECOLOR=%s Screen.Colors()=%d",
		os.Getenv("TERM"), os.Getenv("COLORTERM"), os.Getenv("TCELL_TRUECOLOR"), s.Colors())
	defer s.Fini()

	// initialize and apply theme
	initTheme()
	defStyle := TextStyle
	s.SetStyle(defStyle)
	// paint whole terminal background with themed space
	s.Fill(' ', defStyle)

	chatModule := NewChatModule()
	gameModule := NewGameModule()
	playerModule := NewPlayerModule()
	menuModule := NewMenuModule()
	currentScreen := "menu" // start on the menu screen

	// listen for module updates and re-render
	go func() {
		for {
			select {
			case <-chatModule.Notify():
			case <-gameModule.Notify():
			case <-playerModule.Notify():
			case <-menuModule.Notify():
			}
			renderAll(s, chatModule, gameModule, playerModule, menuModule, currentScreen)
			s.Show()
		}
	}()

	// initial render
	renderAll(s, chatModule, gameModule, playerModule, menuModule, currentScreen)
	s.Show()

	// demo updates removed: mock data and periodic updates have been disabled

	for {
		ev := s.PollEvent()
		switch tev := ev.(type) {
		case *tcell.EventKey:
			// global keys
			if tev.Key() == tcell.KeyEscape || tev.Key() == tcell.KeyCtrlC {
				s.Fini()
				os.Exit(0)
			}
			// toggle menu with 'm'
			if tev.Rune() == 'm' {
				if currentScreen == "game" {
					currentScreen = "menu"
				} else {
					currentScreen = "game"
				}
				renderAll(s, chatModule, gameModule, playerModule, menuModule, currentScreen)
				s.Show()
				continue
			}
			if currentScreen == "game" {
				if tev.Rune() == 'u' {
					chatModule.SendMessage("You", "Manual update")
				}
			} else if currentScreen == "menu" {
				// route navigation keys to menu
				switch tev.Key() {
				case tcell.KeyLeft, tcell.KeyUp:
					menuModule.Move(-1)
				case tcell.KeyRight, tcell.KeyDown:
					menuModule.Move(1)
				case tcell.KeyEnter:
					sel := menuModule.Activate()
					log.Printf("Menu activated selection=%d", sel)
					// simple behavior: go back to game screen after activate
					currentScreen = "game"
				}
			}
		case *tcell.EventResize:
			renderAll(s, chatModule, gameModule, playerModule, menuModule, currentScreen)
			s.Sync()
		}
	}
}

func renderAll(s tcell.Screen, chat *ChatModule, game *GameModule, players *PlayerModule, menu *MenuModule, currentScreen string) {
	// ensure logical buffer uses theme background
	s.Fill(' ', BoxStyle)
	// If menu is active, render it full screen and return
	if currentScreen == "menu" && menu != nil {
		menu.Render(s, 0, 0, TERMINAL_WIDTH, TERMINAL_HEIGHT)
		return
	}
	// w, h := s.Size()

	// leftW := 40
	// rightW := 30
	// centerW := w - leftW - rightW - 4
	// if centerW < 20 {
	// 	centerW = 20
	// }

	// game.Render(s, 0, 0, leftW, h-2)
	// chat.Render(s, leftW+2, 0, centerW, h-2)
	// players.Render(s, leftW+2+centerW+2, 0, rightW, h-2)

	vertSperator := TERMINAL_WIDTH * 2 / 3
	horizSperator := TERMINAL_HEIGHT * 2 / 3

	game.Render(s, 0, 0, vertSperator, horizSperator)
	players.Render(s, 0, horizSperator, vertSperator, TERMINAL_HEIGHT-horizSperator-1)
	chat.Render(s, vertSperator, 0, TERMINAL_WIDTH-vertSperator, TERMINAL_HEIGHT-1)

	instr := "Press Esc/Ctrl+C to quit. Press 'u' to send a manual chat update."
	for i, r := range instr {
		s.SetContent(i, TERMINAL_HEIGHT-1, r, nil, AccentStyle)
	}
}

// tryRunResize attempts to run the resize script with requested rows/cols.
// It's best-effort and will not abort the program if it fails.
func tryRunResize(rows, cols int) {
	// possible script locations relative to repo root or cwd
	candidates := []string{
		"./scripts/resize_and_run.sh",
		"scripts/resize_and_run.sh",
		"/usr/local/bin/resize_and_run.sh",
	}

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			cmd := exec.Command(p, "-r", strconv.Itoa(rows), "-c", strconv.Itoa(cols), "--", "true")
			out, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("resize script %s exited with error: %v\noutput:\n%s", p, err, string(out))
			} else {
				log.Printf("resize script %s output:\n%s", p, string(out))
			}
			return
		}
	}

	log.Printf("resize script not found in candidates; skipping resize attempt")
}

package main

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
)

type GameModule struct {
	mu      sync.RWMutex
	round   int
	notifyC chan struct{}
}

func NewGameModule() *GameModule {
	return &GameModule{round: 0, notifyC: make(chan struct{}, 1)}
}

func (g *GameModule) Notify() <-chan struct{} { return g.notifyC }

func (g *GameModule) SetRound(r int) {
	g.mu.Lock()
	g.round = r
	g.mu.Unlock()
	select {
	case g.notifyC <- struct{}{}:
	default:
	}
}

func (g *GameModule) Render(s tcell.Screen, x, y, w, h int) {
	drawBox(s, x, y, w, h, " Game Screen ")
	g.mu.RLock()
	defer g.mu.RUnlock()
	txt := fmt.Sprintf("Round: %d", g.round)
	drawText(s, x+2, y+2, w-4, txt)
}

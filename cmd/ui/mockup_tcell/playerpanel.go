package main

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
)

type Player struct {
	Name  string
	USD   int
	Bribe int
}

type PlayerModule struct {
	mu      sync.RWMutex
	players map[string]*Player
	notifyC chan struct{}
}

func NewPlayerModule() *PlayerModule {
	pm := &PlayerModule{players: make(map[string]*Player), notifyC: make(chan struct{}, 1)}
	// start with an empty player set; will be populated from API responses
	return pm
}

func (p *PlayerModule) Notify() <-chan struct{} { return p.notifyC }

func (p *PlayerModule) UpdatePlayer(name string, usd int, bribe int) {
	p.mu.Lock()
	pl, ok := p.players[name]
	if !ok {
		pl = &Player{Name: name}
		p.players[name] = pl
	}
	pl.USD = usd
	pl.Bribe = bribe
	p.mu.Unlock()
	select {
	case p.notifyC <- struct{}{}:
	default:
	}
}

func (p *PlayerModule) Render(s tcell.Screen, x, y, w, h int) {
	drawBox(s, x, y, w, h, " Stats ")
	p.mu.RLock()
	defer p.mu.RUnlock()
	i := 0
	for _, pl := range p.players {
		if i >= h-2 {
			break
		}
		line := fmt.Sprintf("%s $%d  bribes:%d", pl.Name, pl.USD, pl.Bribe)
		drawText(s, x+1, y+1+i, w-2, line)
		i++
	}
}

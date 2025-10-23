package main

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
)

type ChatModule struct {
	mu      sync.RWMutex
	lines   []string
	notifyC chan struct{}
}

func NewChatModule() *ChatModule {
	return &ChatModule{lines: []string{"[21:01] Sean: Hi chat !", "[21:02] Alex: Yes, whats up?"}, notifyC: make(chan struct{}, 1)}
}

func (c *ChatModule) Notify() <-chan struct{} { return c.notifyC }

func (c *ChatModule) SendMessage(user, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	line := time.Now().Format("15:04:05") + " " + user + ": " + text
	c.lines = append(c.lines, line)
	select {
	case c.notifyC <- struct{}{}:
	default:
	}
}

func (c *ChatModule) Render(s tcell.Screen, x, y, w, h int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	title := " CHAT "
	drawBox(s, x, y, w, h, title)
	maxLines := h - 2
	start := 0
	if len(c.lines) > maxLines {
		start = len(c.lines) - maxLines
	}
	for i := 0; i < maxLines && start+i < len(c.lines); i++ {
		drawText(s, x+1, y+1+i, w-2, c.lines[start+i])
	}
}

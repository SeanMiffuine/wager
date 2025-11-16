package main

import (
	"sync"

	"github.com/gdamore/tcell/v2"
)

// MenuModule represents the gameplay menu screen with big title and two actions.
type MenuModule struct {
	mu       sync.RWMutex
	selected int // 0 = Create, 1 = Join
	notifyC  chan struct{}
}

func NewMenuModule() *MenuModule {
	return &MenuModule{selected: 0, notifyC: make(chan struct{}, 1)}
}

func (m *MenuModule) Notify() <-chan struct{} { return m.notifyC }

func (m *MenuModule) Move(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected > 1 {
		m.selected = 1
	}
	select {
	case m.notifyC <- struct{}{}:
	default:
	}
}

func (m *MenuModule) Selected() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.selected
}

func (m *MenuModule) Activate() int {
	// return the selected index so caller can act
	return m.Selected()
}

// simple 7x5 pixel font for letters we need in BRIBERY
var bigFont = map[rune][]string{
	'B': {
		"11110",
		"10010",
		"11110",
		"10010",
		"10010",
		"10010",
		"11110",
	},
	'R': {
		"11110",
		"10010",
		"11110",
		"10100",
		"10010",
		"10010",
		"10010",
	},
	'I': {
		"01100",
		"01100",
		"01100",
		"01100",
		"01100",
		"01100",
		"01100",
	},
	'E': {
		"11110",
		"10000",
		"11100",
		"10000",
		"10000",
		"10000",
		"11110",
	},
	'Y': {
		"10001",
		"01010",
		"00100",
		"00100",
		"00100",
		"00100",
		"00100",
	},
}

// drawBigText draws text using the bitmap font and the provided rune as fill.
func drawBigText(s tcell.Screen, x, y int, text string, fill rune, style tcell.Style) {
	// derive letter dimensions from the font pattern and use no extra gap
	ox := x
	for _, ch := range text {
		pattern, ok := bigFont[ch]
		if !ok {
			// unknown char: advance by a single-column spacer
			ox += 1
			continue
		}
		letterHeight := len(pattern)
		// assume each row string length is the letter width
		letterWidth := 0
		if letterHeight > 0 {
			letterWidth = len(pattern[0])
		}
		for ry := 0; ry < letterHeight; ry++ {
			row := pattern[ry]
			for rx := 0; rx < len(row); rx++ {
				if row[rx] == '1' {
					s.SetContent(ox+rx, y+ry, fill, nil, style)
				}
			}
		}
		// no additional gap between letters to keep them tight and legible
		ox += letterWidth
	}
}

// Render draws the menu: bitmap title and two centered action lines.
func (m *MenuModule) Render(s tcell.Screen, x, y, w, h int) {
	// Render the title using the bitmap font for consistent spacing
	title := "BRIBERY"
	// compute title width and height from the font
	titleWidth := 0
	titleHeight := 0
	for _, ch := range title {
		pattern, ok := bigFont[ch]
		if !ok {
			titleWidth += 1
			continue
		}
		if len(pattern) > 0 {
			titleWidth += len(pattern[0])
			if titleHeight == 0 {
				titleHeight = len(pattern)
			}
		}
	}

	if titleHeight == 0 {
		titleHeight = 7
	}

	// center title horizontally; no extra offset so centering is exact
	titleX := x + (w-titleWidth)/2 - 1
	titleY := y + (h-titleHeight)/4 - 3 // position towards the top quarter
	drawBigText(s, titleX, titleY, title, '▒', TextStyle)

	// Draw action lines centered beneath the title
	createText := "Create Room"
	joinText := "Join Room"
	createX := x + (w-len(createText))/2 - 2
	joinX := x + (w-len(joinText))/2 - 2
	createY := titleY + titleHeight + 2
	joinY := createY + 3

	// helper to draw a plain string with TextStyle and BoxStyle for spaces
	drawString := func(px, py int, str string) {
		for i, ch := range str {
			dx := px + i
			if ch == ' ' {
				s.SetContent(dx, py, ' ', nil, TextStyle)
			} else {
				s.SetContent(dx, py, ch, nil, TextStyle)
			}
		}
	}

	drawString(createX, createY, createText)
	drawString(joinX, joinY, joinText)

	// draw selection indicator to the left of the action text using AccentStyle
	sel := m.Selected()
	if sel == 0 {
		s.SetContent(createX-3, createY, '►', nil, TextStyle)
	} else {
		s.SetContent(joinX-3, joinY, '►', nil, TextStyle)
	}
}

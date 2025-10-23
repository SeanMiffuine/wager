package main

import (
	"github.com/gdamore/tcell/v2"
)

func drawBox(s tcell.Screen, x, y, w, h int, title string) {
	// top/bottom borders
	style := BoxStyle
	if w <= 0 || h <= 0 {
		return
	}
	// corners and horizontal
	for i := 0; i < w; i++ {
		s.SetContent(x+i, y, '─', nil, style)
		s.SetContent(x+i, y+h-1, '─', nil, style)
	}
	for j := 0; j < h; j++ {
		s.SetContent(x, y+j, '│', nil, style)
		s.SetContent(x+w-1, y+j, '│', nil, style)
	}
	s.SetContent(x, y, '┌', nil, style)
	s.SetContent(x+w-1, y, '┐', nil, style)
	s.SetContent(x, y+h-1, '└', nil, style)
	s.SetContent(x+w-1, y+h-1, '┘', nil, style)
	// title
	for i, r := range title {
		if x+2+i < x+w-2 {
			s.SetContent(x+2+i, y, r, nil, style)
		}
	}
}

func drawText(s tcell.Screen, x, y, w int, text string) {
	if w <= 0 {
		return
	}
	runes := []rune(text)
	if len(runes) > w {
		runes = runes[:w]
	}
	for i, r := range runes {
		s.SetContent(x+i, y, r, nil, TextStyle)
	}
}

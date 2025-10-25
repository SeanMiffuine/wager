package main

import "github.com/gdamore/tcell/v2"

// Theme colors and styles for the mockup tcell UI.
var (
	ThemeBg     tcell.Color
	ThemeFg     tcell.Color
	ThemeAccent tcell.Color

	BoxStyle    tcell.Style
	TextStyle   tcell.Style
	AccentStyle tcell.Style
)

// Preserve extended-ASCII shading runes for later use (kept as runes).
const (
	Ascii176 = rune(176) // extended-ASCII 176 (light shade) — kept for future use
	Ascii177 = rune(177) // extended-ASCII 177 (medium shade)
	Ascii178 = rune(178) // extended-ASCII 178 (dark shade)
)

// initTheme initializes a simple uniform theme. Call before rendering.
func initTheme() {
	// Pick whatever RGB/hex values you like. TrueColor() forces RGB fidelity
	// on terminals that support truecolor.
	ThemeBg = tcell.NewRGBColor(175, 167, 165)     // light brown
	ThemeFg = tcell.NewRGBColor(55, 47, 47)        // dakr brown
	ThemeAccent = tcell.NewRGBColor(184, 178, 157) // slightly less lgiht brown

	BoxStyle = tcell.StyleDefault.Foreground(ThemeFg).Background(ThemeBg)
	TextStyle = tcell.StyleDefault.Foreground(ThemeFg).Background(ThemeBg)
	AccentStyle = tcell.StyleDefault.Foreground(ThemeAccent).Background(ThemeBg)
}

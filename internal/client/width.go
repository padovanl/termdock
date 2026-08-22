package client

import (
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

// A terminal cell is not a rune. Emoji occupy two columns while counting
// as one rune (🔋), some count as two runes and occupy two columns
// (🖥️ — a base plus a variation selector), and CJK text is two columns
// throughout. Measuring text by len([]rune(...)) is therefore wrong
// twice over, and it was wrong in both directions here: the status bar's
// right-aligned segment landed a column or two off, and text drawn a
// rune at a time had its second half overwritten by whatever came next.
//
// uniseg does the real thing — grapheme clusters and East Asian width —
// and comes in with tcell already, so this costs nothing but the import.

// textWidth is how many terminal columns s occupies.
func textWidth(s string) int { return uniseg.StringWidth(s) }

// runeWidth is how many columns r occupies. Zero for a combining mark or
// a variation selector, which attach to the glyph before them rather
// than taking space of their own.
func runeWidth(r rune) int { return uniseg.StringWidth(string(r)) }

// readableOn picks a foreground that can actually be read on bg.
//
// Highlights were black text on the theme's accent colour, whatever that
// accent happened to be. That is fine on Nord's pale frost blue and poor
// on Solarized's mid blue or Rosé Pine's iris — the selected row in a
// picker, the active window tab and the quick-jump badges all went hard
// to read depending on the theme, which is exactly the sort of thing a
// fixed colour cannot get right for eleven palettes.
//
// Relative luminance per WCAG, then black or white whichever is further
// from it. Not a full contrast-ratio solve — just the choice between the
// only two options a terminal reliably has, made per colour instead of
// once for all of them.
func readableOn(bg tcell.Color) tcell.Color {
	h := bg.Hex()
	if h < 0 {
		return tcell.ColorWhite // an unresolvable colour: assume a dark terminal
	}
	lin := func(c float64) float64 {
		c /= 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r := lin(float64((h >> 16) & 0xff))
	g := lin(float64((h >> 8) & 0xff))
	b := lin(float64(h & 0xff))
	if 0.2126*r+0.7152*g+0.0722*b > 0.45 {
		return tcell.ColorBlack
	}
	return tcell.ColorWhite
}

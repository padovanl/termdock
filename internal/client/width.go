package client

import "github.com/rivo/uniseg"

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

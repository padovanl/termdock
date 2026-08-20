package core

import "regexp"

// compileSearch turns a user-typed query into a case-insensitive
// matcher shared by copy-mode's own "/" (see searchNext) and the global
// scrollback search (Ctrl-B /, see refilterSearch): the query is tried
// as a Go regular expression first — the same "your search doubles as a
// pattern" convention grep/vim/less all use — and only falls back to a
// literal, case-insensitive substring match if it fails to compile
// (e.g. an unbalanced "(" or a bare "*"), so a plain-text query behaves
// exactly as it always did while one that happens to use regex syntax
// gets real regex power. Returns nil for an empty query (nothing to
// search for).
func compileSearch(query string) *regexp.Regexp {
	if query == "" {
		return nil
	}
	if re, err := regexp.Compile("(?i)" + query); err == nil {
		return re
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
}

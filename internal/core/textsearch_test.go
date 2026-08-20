package core

import "testing"

func TestCompileSearchEmptyQueryIsNil(t *testing.T) {
	if compileSearch("") != nil {
		t.Fatal("an empty query should produce no matcher at all")
	}
}

func TestCompileSearchIsCaseInsensitive(t *testing.T) {
	re := compileSearch("hello")
	if re == nil || !re.MatchString("say HELLO world") {
		t.Fatal("a plain query should match case-insensitively, same as before regex support")
	}
}

func TestCompileSearchValidRegexUsesRegexSemantics(t *testing.T) {
	re := compileSearch(`err(or)?-\d+`)
	if re == nil {
		t.Fatal("a valid regex should compile")
	}
	if !re.MatchString("error-42") {
		t.Fatal("err(or)?-\\d+ should match \"error-42\"")
	}
	if !re.MatchString("err-7") {
		t.Fatal("err(or)?-\\d+ should also match \"err-7\" (the optional group)")
	}
	if re.MatchString("errorx-42") {
		t.Fatal("err(or)?-\\d+ should not match \"errorx-42\" (extra character before the dash)")
	}
}

func TestCompileSearchInvalidRegexFallsBackToLiteral(t *testing.T) {
	re := compileSearch("a(b") // unbalanced group: not valid regex syntax
	if re == nil {
		t.Fatal("an invalid regex should still produce a (literal) matcher, not nil")
	}
	if !re.MatchString("xxa(bxx") {
		t.Fatal("the literal fallback should match the exact text \"a(b\" as a substring")
	}
	if re.MatchString("axb") {
		t.Fatal("the literal fallback must not treat '(' as a regex metacharacter")
	}
}

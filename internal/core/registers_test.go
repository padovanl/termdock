package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func typeRegisterQuery(c *Core, s string) {
	for _, r := range s {
		c.handleRegisterKey(tcell.KeyRune, r)
	}
}

func TestPasteRegisterUsesMostRecentYank(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	if len(c.registers) != 0 {
		t.Fatalf("expected no registers yet, got %d", len(c.registers))
	}
	c.pushRegister("first")
	c.pushRegister("second")
	c.mu.Unlock()

	if len(c.registers) != 2 {
		t.Fatalf("expected 2 registers, got %d", len(c.registers))
	}
	if c.registers[0].text != "second" {
		t.Fatalf("most recent register should be first in the list, got %q", c.registers[0].text)
	}
}

func TestRegistersRingEvictsOldest(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	for i := 0; i < maxRegisters+5; i++ {
		c.pushRegister(string(rune('a' + i%26)))
	}
	n := len(c.registers)
	newest := c.registers[0].text
	c.mu.Unlock()

	if n != maxRegisters {
		t.Fatalf("expected the ring capped at %d, got %d", maxRegisters, n)
	}
	if newest != string(rune('a'+(maxRegisters+4)%26)) {
		t.Fatalf("newest register = %q, want the last one pushed", newest)
	}
}

func TestRegisterPickerFiltersAndPastes(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.pushRegister("hello world")
	c.pushRegister("goodbye world")
	c.enterRegisterPicker()
	if c.mode != ModeRegisters {
		t.Fatalf("enterRegisterPicker should set ModeRegisters, got %v", c.mode)
	}
	typeRegisterQuery(c, "hello")
	filtered := len(c.regPicker.filtered)
	c.mu.Unlock()

	if filtered != 1 {
		t.Fatalf("query 'hello' should match exactly 1 register, matched %d", filtered)
	}

	c.mu.Lock()
	c.handleRegisterKey(tcell.KeyEnter, 0)
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("confirming should return to ModeNormal, got %v", mode)
	}
}

func TestEnterRegisterPickerWithNoRegistersIsANoop(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.enterRegisterPicker()
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("opening the register picker with nothing to show should stay in ModeNormal, got %v", mode)
	}
}

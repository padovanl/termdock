package core

import "github.com/gdamore/tcell/v2"

// inputState is a one-line text prompt: renaming a window and starting a
// scrollback search both need "type something, Enter confirms, Esc
// cancels", so they share this rather than each inventing their own.
type inputState struct {
	purpose    string // "rename" | "search"
	prompt     string
	buffer     []rune
	returnMode Mode // mode to go back to on confirm/cancel
}

func (c *Core) startInput(purpose, prompt, prefill string, returnMode Mode) {
	c.mode = ModeInput
	c.input = inputState{
		purpose:    purpose,
		prompt:     prompt,
		buffer:     []rune(prefill),
		returnMode: returnMode,
	}
}

func (c *Core) handleInputKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc:
		c.cancelInput()
	case key == tcell.KeyEnter:
		c.confirmInput()
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.input.buffer); n > 0 {
			c.input.buffer = c.input.buffer[:n-1]
		}
	case key == tcell.KeyCtrlU:
		c.input.buffer = c.input.buffer[:0]
	case r != 0 && key == tcell.KeyRune:
		c.input.buffer = append(c.input.buffer, r)
	}
}

func (c *Core) cancelInput() {
	c.mode = c.input.returnMode
	c.input = inputState{}
}

func (c *Core) confirmInput() {
	text := string(c.input.buffer)
	switch c.input.purpose {
	case "rename":
		w := c.win()
		if text == "" {
			w.renamed = false
			w.Name = ""
		} else {
			w.Name = text
			w.renamed = true
		}
		c.persistStateLocked()
	case "rename-pane":
		// An empty name clears it, putting the pane back to being titled
		// after whatever is running in it — which is the sensible way to
		// undo a rename, and what an empty prompt plainly means.
		id := c.win().active.ID
		if text == "" {
			delete(c.paneNames, id)
			c.statusMsg = "pane name cleared"
		} else {
			if c.paneNames == nil {
				c.paneNames = map[int]string{}
			}
			c.paneNames[id] = text
		}
		c.persistStateLocked()
	case "log-window":
		c.applyWindowLogging(text)
	case "rename-session":
		c.applySessionRename(text)
	case "search":
		c.copy.searchTerm = text
		if text != "" {
			c.searchNext(1)
		}
	case "cmd":
		if text != "" {
			c.runCommand(text)
		}
	}
	c.mode = c.input.returnMode
	c.input = inputState{}
}

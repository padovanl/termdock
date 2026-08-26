package server

import (
	"runtime"
	"testing"
	"time"
)

// Attaching and detaching is the most repeated thing a session does over
// its life: reattaching from a new terminal every morning for a month is
// dozens of round trips through handleConn. A goroutine left behind by
// each one is a leak that only surfaces in a session old enough that
// restarting it loses real work, which is the kind this daemon exists to
// avoid losing.
func TestAttachAndDetachDoNotLeakGoroutines(t *testing.T) {
	sock, kill := startSession(t, "leak-probe")
	defer kill()

	cycle := func() {
		conn, _, _ := dial(t, sock, false)
		conn.Close()
	}

	settle := func() int {
		for i := 0; i < 50; i++ {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}

	for i := 0; i < 3; i++ { // warm up: the first attach builds shared state
		cycle()
	}
	base := settle()

	for i := 0; i < 15; i++ {
		cycle()
	}
	after := settle()

	t.Logf("goroutines: %d before, %d after 15 attach/detach cycles", base, after)
	if after > base+3 {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutines went %d -> %d over 15 attach/detach cycles\n%s", base, after, buf[:n])
	}
}

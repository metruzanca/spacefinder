package tui

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// delayedKeyReader emits a single key byte after a delay, then EOF.
type delayedKeyReader struct {
	timeout time.Duration
	key     byte
	done    bool
}

func (d *delayedKeyReader) Read(p []byte) (int, error) {
	if d.done {
		return 0, io.EOF
	}
	d.done = true
	time.Sleep(d.timeout)
	p[0] = d.key
	return 1, nil
}

// TestReproHomeUptime drives the real tea.Program against the user's home dir
// repeatedly (opt-in, SF_REPRO=1) to surface the intermittent panic bubbletea
// reports as "program experienced a panic". Panics are recovered by bubbletea
// and printed to the test log.
func TestReproHomeUptime(t *testing.T) {
	if os.Getenv("SF_REPRO") == "" {
		t.Skip("set SF_REPRO=1 to run the reproduction")
	}
	for i := 1; i <= 8; i++ {
		var out bytes.Buffer
		p := tea.NewProgram(newModel("/home/metru"),
			tea.WithInput(&delayedKeyReader{timeout: 7 * time.Second, key: 'q'}),
			tea.WithOutput(&out),
			tea.WithoutRenderer(),
			tea.WithMouseCellMotion(),
		)
		_, err := p.Run()
		t.Logf("iter %d: err=%v bytes=%d", i, err, out.Len())
		if err != nil {
			t.Fatalf("panic on iter %d: %v", i, err)
		}
	}
}

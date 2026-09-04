package tui

import (
	"context"
	"math/rand"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/metruzanca/spacefinder/internal/scan"
)

// TestStressViewRandomSizes hammers buildLayout/View/Update with random
// terminal sizes and mouse events over a real measured tree, to flush out
// latent index or bounds panics in the render path. Opt-in: run with
// SF_STRESS=1 and, ideally, -race.
func TestStressViewRandomSizes(t *testing.T) {
	if os.Getenv("SF_STRESS") == "" {
		t.Skip("set SF_STRESS=1 to run the render stress test")
	}
	root := "/home/metru/.npm"
	if _, err := os.Stat(root); err != nil {
		t.Skipf("benchmark root %s unavailable: %v", root, err)
	}
	sc, tree, err := scan.Measure(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	rnd := rand.New(rand.NewSource(1))
	const iters = 20000
	for i := 0; i < iters; i++ {
		m := newModel(root)
		m.mode = modeBrowse
		m.scanner = sc
		m.tree = tree
		m.current = tree
		m.width = 10 + rnd.Intn(190)
		m.height = 6 + rnd.Intn(70)
		m.buildLayout()
		_ = m.View()
		switch rnd.Intn(6) {
		case 0:
			m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: rnd.Intn(m.width + 40), Y: rnd.Intn(m.height + 40)})
		case 1:
			m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		case 2:
			m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
		case 3:
			m.moveSel(1, 0)
		case 4:
			m.moveSel(0, 1)
		case 5:
			m.updateBrowse(keyRunes("j"))
		}
		_ = m.View()
	}
}

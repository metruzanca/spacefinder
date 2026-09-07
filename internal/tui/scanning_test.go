package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/metruzanca/spacefinder/internal/scan"
)

// scanningModel builds a scan-mode model with the given completed children,
// springs snapped to their final sizes so rendering is deterministic.
func scanningModel(children []scan.ChildSize) *Model {
	m := newModel("/root")
	m.mode = modeScanning
	m.width, m.height = 80, 24
	m.start = time.Now().Add(-3 * time.Second)
	m.progress = scan.Progress{Visited: 12345, Current: "/root/docs"}
	if len(children) > 0 {
		m.addScanChildren(children)
		m.settleScanSprings()
	}
	return m
}

func TestScanBlocksAppearInOrder(t *testing.T) {
	m := scanningModel(nil)
	if m.selectedNode() != nil {
		t.Fatal("no children yet; selection should be nil")
	}
	// First block explored fills the whole map on its own.
	m.addScanChildren([]scan.ChildSize{{Name: "docs", Size: 400}})
	if len(m.scanChildren()) != 1 || m.scanChildren()[0].Name != "docs" {
		t.Fatalf("first child not added: %+v", m.scanChildren())
	}
	if m.selectedNode() == nil || m.selectedNode().Name != "docs" {
		t.Fatalf("selection = %+v, want docs", m.selectedNode())
	}
	if m.current != m.tree || m.tree != m.scanRoot {
		t.Fatal("scan mode should render against the synthetic root")
	}
	// A second child completes later, in exploration order.
	m.addScanChildren([]scan.ChildSize{{Name: "media", Size: 200}})
	if len(m.scanChildren()) != 2 {
		t.Fatalf("second child not added: %+v", m.scanChildren())
	}
	if got := m.scanChildren()[1].Name; got != "media" {
		t.Fatalf("second child = %q, want media", got)
	}
}

func TestScanSpringGrowsToTarget(t *testing.T) {
	m := scanningModel(nil)
	m.addScanChildren([]scan.ChildSize{{Name: "docs", Size: 400}})
	target := m.scanSprings[0].target
	if m.scanSprings[0].pos != scanInitialPos {
		t.Fatalf("new block pos = %f, want sliver %d", m.scanSprings[0].pos, scanInitialPos)
	}
	prev := m.scanSprings[0].pos
	for i := 0; i < 20; i++ {
		if !m.stepScanSprings() {
			break
		}
		pos := m.scanSprings[0].pos
		if pos < prev {
			t.Fatalf("block shrank: %f -> %f", prev, pos)
		}
		prev = pos
	}
	if m.scanSprings[0].pos >= target {
		t.Fatalf("block reached target too early: pos=%f target=%f", m.scanSprings[0].pos, target)
	}
	m.settleScanSprings()
	if m.scanSprings[0].pos != target {
		t.Fatalf("settled pos = %f, want target %f", m.scanSprings[0].pos, target)
	}
}

func TestScanTickerArmsAndStops(t *testing.T) {
	m := scanningModel(nil)
	m.addScanChildren([]scan.ChildSize{{Name: "docs", Size: 400}}) // not settled: block is growing
	if cmd := m.armScanTicker(); cmd == nil {
		t.Fatal("armScanTicker should return a frame cmd when idle")
	}
	if cmd := m.armScanTicker(); cmd != nil {
		t.Fatal("armScanTicker should not double-arm while ticking")
	}
	// A frame while the block is still growing re-arms; once settled it stops.
	got, cmd := m.Update(scanFrameMsg(time.Now()))
	m = got.(*Model)
	if cmd == nil {
		t.Fatal("expected a re-armed frame cmd while the block is growing")
	}
	if !m.scanTicking {
		t.Fatal("ticker should still be armed while moving")
	}
	m.settleScanSprings()
	got, cmd = m.Update(scanFrameMsg(time.Now()))
	m = got.(*Model)
	if cmd != nil {
		t.Fatal("settled frame should not re-arm the ticker")
	}
	if m.scanTicking {
		t.Fatal("ticker should stop once blocks settle")
	}
}

func TestScanDoneClearsAnimation(t *testing.T) {
	m := scanningModel([]scan.ChildSize{{Name: "docs", Size: 400}})
	if len(m.scanSprings) == 0 {
		t.Fatal("animation state should exist during the scan")
	}
	tree := testTree()
	got, _ := m.Update(scanDoneMsg{gen: 0, root: tree})
	m = got.(*Model)
	if m.mode != modeBrowse {
		t.Fatalf("mode = %v, want modeBrowse after scan completes", m.mode)
	}
	if m.scanRoot != nil || m.scanSprings != nil || m.scanTicking {
		t.Fatal("scan animation state should be cleared on scan completion")
	}
}

func TestViewScanningGolden(t *testing.T) {
	golden(t, "scan_1child", scanningModel([]scan.ChildSize{{Name: "docs", Size: 400}}).View())
	golden(t, "scan_2children", scanningModel([]scan.ChildSize{
		{Name: "docs", Size: 400},
		{Name: "media", Size: 200},
	}).View())
}

func TestViewScanningPlaceholder(t *testing.T) {
	m := scanningModel(nil)
	v := m.View()
	if v == "" {
		t.Fatal("placeholder view empty")
	}
	if !strings.Contains(v, "Scanning") {
		t.Fatalf("placeholder should announce the scan; got %q", v)
	}
	// Once children arrive the live treemap replaces the placeholder.
	m.addScanChildren([]scan.ChildSize{{Name: "docs", Size: 400}})
	m.settleScanSprings()
	v = m.View()
	if !strings.Contains(v, "scanning") {
		t.Fatalf("scan info line missing; got %q", v)
	}
}

func TestScanProgressMsgTriggersLayout(t *testing.T) {
	m := scanningModel(nil)
	got, cmd := m.Update(scanProgressMsg(scan.Progress{
		Visited:   10,
		Completed: []scan.ChildSize{{Name: "docs", Size: 400}},
	}))
	m = got.(*Model)
	if len(m.scanChildren()) != 1 {
		t.Fatalf("progress did not add the child: %+v", m.scanChildren())
	}
	if cmd == nil {
		t.Fatal("expected a batch cmd (frame ticker + redeliver)")
	}
	if len(m.rects) == 0 {
		t.Fatal("progress should have produced a live layout")
	}
}

func TestScanLayoutFillsGrid(t *testing.T) {
	m := scanningModel(nil)
	m.addScanChildren([]scan.ChildSize{{Name: "docs", Size: 400}})
	m.settleScanSprings()
	if len(m.rects) != 1 {
		t.Fatalf("one child should yield one rect, got %d", len(m.rects))
	}
	w, h := m.treemapSize()
	r := m.rects[0]
	if r.W != float64(w) || r.H != float64(h) {
		t.Fatalf("first block should fill the whole grid, got %gx%g want %dx%d", r.W, r.H, w, h)
	}
	// A second child takes its proportional share; the first stays larger.
	m.addScanChildren([]scan.ChildSize{{Name: "media", Size: 200}})
	m.settleScanSprings()
	if len(m.rects) != 2 {
		t.Fatalf("two children should yield two rects, got %d", len(m.rects))
	}
	a := m.rects[0].W * m.rects[0].H
	b := m.rects[1].W * m.rects[1].H
	if a <= b {
		t.Fatalf("docs (400) should outsize media (200): areas %f vs %f", a, b)
	}
}

func TestScanBigBlockMapsToItsRect(t *testing.T) {
	m := scanningModel(nil)
	// A tiny file is explored first; a huge directory completes later. The
	// huge block must end up as the largest rect, with labels/selection
	// resolving to it (LayoutFixed sorts by size, so rect.Index alone does not
	// equal the scanChildren index — the pageIdx/ordered mapping is required).
	m.addScanChildren([]scan.ChildSize{{Name: "small", Size: 4 * 1024}})
	m.settleScanSprings()
	m.addScanChildren([]scan.ChildSize{{Name: "huge", Size: 60 * 1024 * 1024 * 1024}})
	m.settleScanSprings()
	if len(m.rects) != 2 {
		t.Fatalf("want 2 rects, got %d", len(m.rects))
	}
	big := m.rects[0]
	hugeIdx := 1 // scanChildren order: small=0, huge=1
	if got := m.pageIdx[big.Index]; got != hugeIdx {
		t.Fatalf("largest rect maps to child %d, want huge (%d); pageIdx=%v", got, hugeIdx, m.pageIdx)
	}
	m.sel = 0
	if n := m.selectedNode(); n == nil || n.Name != "huge" {
		t.Fatalf("selection behind biggest rect = %v, want huge", n)
	}
	if a, b := m.rects[0].W*m.rects[0].H, m.rects[1].W*m.rects[1].H; a <= b {
		t.Fatalf("huge rect should outsize small rect: %f vs %f", a, b)
	}
}

func TestScanInfoLineDeterministic(t *testing.T) {
	m := scanningModel([]scan.ChildSize{{Name: "docs", Size: 400}})
	line := m.infoLine()
	for _, want := range []string{"scanning", "1 children", "so far", "visited 12345", "3s"} {
		if !strings.Contains(line, want) {
			t.Fatalf("info line %q missing %q", line, want)
		}
	}
}

func TestScanViewIsTreemap(t *testing.T) {
	m := scanningModel([]scan.ChildSize{{Name: "docs", Size: 400}})
	v := m.View()
	if strings.Count(v, "█") < 20 {
		t.Fatalf("scan view should render solid blocks; found %d", strings.Count(v, "█"))
	}
}
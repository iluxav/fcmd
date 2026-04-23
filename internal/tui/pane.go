package tui

import (
	"fmt"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"fcmd/internal/discovery"
	"fcmd/internal/proto"
	"fcmd/internal/vfs"
)

type paneMode int

const (
	modeLocal paneMode = iota
	modeHosts
	modeRemoteRoots
	modeRemote
)

type pane struct {
	app *App

	title *tview.TextView
	table *tview.Table
	flex  *tview.Flex

	mode paneMode
	fs   vfs.FS

	// remote-specific
	hosts []discovery.Host
	roots []proto.Root

	cwd          string
	entries      []vfs.Entry // raw listing from the FS
	displayRows  []vfs.Entry // what the table renders (may include "..")
	selected     map[string]bool
}

func newPane(app *App, localStart bool) *pane {
	p := &pane{
		app:      app,
		title:    tview.NewTextView().SetDynamicColors(true),
		table:    tview.NewTable().SetSelectable(true, false).SetFixed(1, 0),
		selected: map[string]bool{},
	}
	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.title, 1, 0, false).
		AddItem(p.table, 0, 1, true)

	if localStart {
		p.fs = vfs.NewLocal()
		p.mode = modeLocal
		home, _ := homeDir()
		p.cwd = home
	} else {
		p.mode = modeHosts
	}

	p.table.SetBorder(true)
	p.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite))
	p.table.SetSelectionChangedFunc(func(row, col int) {
		// no-op; kept to allow future status updates
	})

	p.table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEnter:
			p.onEnter()
			return nil
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			p.onBackspace()
			return nil
		case tcell.KeyEsc:
			p.onEsc()
			return nil
		}
		switch ev.Rune() {
		case ' ':
			p.toggleSelection()
			return nil
		}
		return ev
	})

	return p
}

func (p *pane) refresh() {
	row, col := p.table.GetSelection()
	p.table.Clear()
	p.renderTitle()
	p.renderHeader()
	switch p.mode {
	case modeHosts:
		p.renderHosts()
	case modeRemoteRoots:
		p.renderRoots()
	default:
		p.renderEntries()
	}
	// Preserve the caller's cursor position, clamped to the new row range.
	nRows := p.table.GetRowCount()
	if nRows <= 1 {
		return
	}
	if row < 1 {
		row = 1
	}
	if row >= nRows {
		row = nRows - 1
	}
	p.table.Select(row, col)
}

// resetCursor moves selection to the first data row and scrolls to the top.
// Call after loading a fresh listing (navigating, reconnecting, etc.).
func (p *pane) resetCursor() {
	p.table.ScrollToBeginning()
	if p.table.GetRowCount() > 1 {
		p.table.Select(1, 0)
	}
}

func (p *pane) renderTitle() {
	var label string
	switch p.mode {
	case modeLocal:
		label = fmt.Sprintf("[yellow::b]Local[-::-]  %s", p.cwd)
	case modeHosts:
		label = "[yellow::b]LAN Hosts[-::-]  (Enter: connect)"
	case modeRemoteRoots:
		label = fmt.Sprintf("[yellow::b]%s[-::-]  (roots)", p.fs.Label())
	case modeRemote:
		label = fmt.Sprintf("[yellow::b]%s[-::-]  %s", p.fs.Label(), p.cwd)
	}
	p.title.SetText(label)
}

func (p *pane) renderHeader() {
	p.table.SetCell(0, 0, headerCell("  "))
	p.table.SetCell(0, 1, headerCell("Name"))
	p.table.SetCell(0, 2, headerCell("Size").SetAlign(tview.AlignRight))
	p.table.SetCell(0, 3, headerCell("Bytes").SetAlign(tview.AlignRight))
}

func headerCell(text string) *tview.TableCell {
	return tview.NewTableCell(text).
		SetSelectable(false).
		SetTextColor(tcell.ColorAqua).
		SetAttributes(tcell.AttrBold)
}

func (p *pane) renderHosts() {
	if len(p.hosts) == 0 {
		p.table.SetCell(1, 1, tview.NewTableCell("(scanning — press r to rescan)").SetTextColor(tcell.ColorGray))
		return
	}
	for i, h := range p.hosts {
		row := i + 1
		p.table.SetCell(row, 0, tview.NewTableCell(" *"))
		p.table.SetCell(row, 1, tview.NewTableCell(fmt.Sprintf("%s (%s)", h.Name, h.Addr)).SetTextColor(tcell.ColorLightGreen))
		p.table.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf(":%d", h.Port)).SetAlign(tview.AlignRight).SetTextColor(tcell.ColorGray))
		p.table.SetCell(row, 3, tview.NewTableCell(""))
	}
}

func (p *pane) renderRoots() {
	for i, r := range p.roots {
		row := i + 1
		p.table.SetCell(row, 0, tview.NewTableCell(" D"))
		p.table.SetCell(row, 1, tview.NewTableCell(r.Name).SetTextColor(tcell.ColorLightSkyBlue))
		p.table.SetCell(row, 2, tview.NewTableCell("<DIR>").SetAlign(tview.AlignRight).SetTextColor(tcell.ColorGray))
		p.table.SetCell(row, 3, tview.NewTableCell(r.Path).SetAlign(tview.AlignRight).SetTextColor(tcell.ColorGray))
	}
}

func (p *pane) renderEntries() {
	raw := append([]vfs.Entry(nil), p.entries...)
	sort.SliceStable(raw, func(i, j int) bool {
		if raw[i].IsDir != raw[j].IsDir {
			return raw[i].IsDir
		}
		return raw[i].Name < raw[j].Name
	})
	display := raw
	if p.canGoUp() {
		display = append([]vfs.Entry{{Name: "..", IsDir: true}}, raw...)
	}
	p.displayRows = display
	for i, e := range display {
		row := i + 1
		mark := "  "
		if p.selected[e.Name] {
			mark = " *"
		}
		icon := " F"
		if e.IsDir {
			icon = " D"
		}
		name := e.Name
		color := tcell.ColorWhite
		if e.IsDir {
			color = tcell.ColorLightSkyBlue
		}
		if p.selected[e.Name] {
			color = tcell.ColorYellow
		}
		p.table.SetCell(row, 0, tview.NewTableCell(mark+icon[1:]).SetTextColor(color))
		p.table.SetCell(row, 1, tview.NewTableCell(name).SetTextColor(color))
		sizeStr := "<DIR>"
		byteStr := ""
		if !e.IsDir {
			sizeStr = humanSize(e.Size)
			byteStr = fmt.Sprintf("%d", e.Size)
		}
		p.table.SetCell(row, 2, tview.NewTableCell(sizeStr).SetAlign(tview.AlignRight).SetTextColor(tcell.ColorGray))
		p.table.SetCell(row, 3, tview.NewTableCell(byteStr).SetAlign(tview.AlignRight).SetTextColor(tcell.ColorGray))
	}
}

func (p *pane) canGoUp() bool {
	if p.mode == modeLocal {
		return p.fs.Parent(p.cwd) != p.cwd
	}
	if p.mode == modeRemote {
		return true
	}
	return false
}

func (p *pane) currentRow() int {
	r, _ := p.table.GetSelection()
	return r
}

func (p *pane) currentEntryName() (string, bool) {
	r := p.currentRow()
	if r <= 0 {
		return "", false
	}
	switch p.mode {
	case modeHosts:
		i := r - 1
		if i < 0 || i >= len(p.hosts) {
			return "", false
		}
		return p.hosts[i].Name, true
	case modeRemoteRoots:
		i := r - 1
		if i < 0 || i >= len(p.roots) {
			return "", false
		}
		return p.roots[i].Name, true
	default:
		i := r - 1
		if i < 0 || i >= len(p.displayRows) {
			return "", false
		}
		return p.displayRows[i].Name, true
	}
}

func (p *pane) currentEntry() (vfs.Entry, bool) {
	r := p.currentRow()
	if r <= 0 {
		return vfs.Entry{}, false
	}
	i := r - 1
	if i < 0 || i >= len(p.displayRows) {
		return vfs.Entry{}, false
	}
	return p.displayRows[i], true
}

func (p *pane) onEnter() {
	switch p.mode {
	case modeHosts:
		r := p.currentRow() - 1
		if r < 0 || r >= len(p.hosts) {
			return
		}
		p.connectTo(p.hosts[r])
	case modeRemoteRoots:
		r := p.currentRow() - 1
		if r < 0 || r >= len(p.roots) {
			return
		}
		p.cwd = p.roots[r].Path
		p.mode = modeRemote
		p.selected = map[string]bool{}
		p.loadList()
		p.resetCursor()
	case modeLocal, modeRemote:
		e, ok := p.currentEntry()
		if !ok {
			return
		}
		if e.Name == ".." {
			p.onBackspace()
			return
		}
		if !e.IsDir {
			return
		}
		p.cwd = p.fs.Join(p.cwd, e.Name)
		p.selected = map[string]bool{}
		p.loadList()
		p.resetCursor()
	}
}

func (p *pane) onBackspace() {
	switch p.mode {
	case modeLocal, modeRemote:
		parent := p.fs.Parent(p.cwd)
		if parent == p.cwd {
			if p.mode == modeRemote {
				p.mode = modeRemoteRoots
				p.selected = map[string]bool{}
				p.refresh()
				p.resetCursor()
			}
			return
		}
		p.cwd = parent
		p.selected = map[string]bool{}
		p.loadList()
		p.resetCursor()
	}
}

func (p *pane) onEsc() {
	// In remote panes, Esc pops back to host browser.
	if p.mode == modeRemote || p.mode == modeRemoteRoots {
		if p.fs != nil {
			p.fs.Close()
		}
		p.fs = nil
		p.roots = nil
		p.entries = nil
		p.selected = map[string]bool{}
		p.mode = modeHosts
		p.app.scanHosts()
		p.refresh()
		p.resetCursor()
	}
}

func (p *pane) toggleSelection() {
	e, ok := p.currentEntry()
	if !ok {
		return
	}
	if e.Name == ".." {
		// advance cursor without selecting
		p.table.Select(p.currentRow()+1, 0)
		return
	}
	if p.selected[e.Name] {
		delete(p.selected, e.Name)
	} else {
		p.selected[e.Name] = true
	}
	p.refresh()
	p.table.Select(p.currentRow()+1, 0)
}

func (p *pane) loadList() {
	if p.fs == nil {
		return
	}
	entries, err := p.fs.List(p.cwd)
	if err != nil {
		p.app.showError(fmt.Sprintf("list %s: %v", p.cwd, err))
		p.entries = nil
	} else {
		p.entries = entries
	}
	p.refresh()
}

func (p *pane) connectTo(h discovery.Host) {
	r, err := vfs.NewRemote(h.Endpoint(), h.Name)
	if err != nil {
		p.app.showError(fmt.Sprintf("connect %s: %v", h.Endpoint(), err))
		return
	}
	p.fs = r
	roots, err := r.Roots()
	if err != nil {
		p.app.showError(fmt.Sprintf("roots: %v", err))
		r.Close()
		p.fs = nil
		return
	}
	p.roots = roots
	p.mode = modeRemoteRoots
	p.refresh()
	p.resetCursor()
}

// selectionPaths returns the absolute paths of selected items, or the current one if none.
func (p *pane) selectionPaths() []string {
	if p.mode != modeLocal && p.mode != modeRemote {
		return nil
	}
	var out []string
	if len(p.selected) > 0 {
		for name := range p.selected {
			out = append(out, p.fs.Join(p.cwd, name))
		}
	} else {
		e, ok := p.currentEntry()
		if !ok || e.Name == ".." {
			return nil
		}
		out = append(out, p.fs.Join(p.cwd, e.Name))
	}
	sort.Strings(out)
	return out
}

func humanSize(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case n >= TB:
		return fmt.Sprintf("%.1f TB", float64(n)/float64(TB))
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

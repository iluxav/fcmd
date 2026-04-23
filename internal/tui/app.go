package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"fcmd/internal/discovery"
	"fcmd/internal/vfs"
)

type App struct {
	tv     *tview.Application
	pages  *tview.Pages
	root   *tview.Flex
	left   *pane
	right  *pane
	status *tview.TextView
	active *pane
}

func Run() error {
	a := &App{tv: tview.NewApplication()}
	a.left = newPane(a, true)
	a.right = newPane(a, false)

	body := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.left.flex, 0, 1, true).
		AddItem(a.right.flex, 0, 1, false)

	a.status = tview.NewTextView().SetDynamicColors(true).SetRegions(false)
	a.status.SetText(helpLine())

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(body, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.pages = tview.NewPages().AddPage("main", a.root, true, true)

	a.active = a.left
	a.tv.SetFocus(a.left.table)
	a.highlightActive()

	a.tv.SetInputCapture(a.globalKeys)

	a.left.loadList()
	a.left.resetCursor()
	a.scanHosts()
	a.right.refresh()
	a.right.resetCursor()

	a.tv.SetRoot(a.pages, true).EnableMouse(false)
	return a.tv.Run()
}

func homeDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "/", err
	}
	return h, nil
}

func helpLine() string {
	return "[aqua]←/→[-]:switch  [aqua]Enter[-]:open  [aqua]Bksp[-]:up  [aqua]Esc[-]:hosts  " +
		"[aqua]Space[-]:select  [aqua]^⇧C/C[-]:copy  [aqua]^⇧M/M[-]:move  [aqua]F7[-]:mkdir  " +
		"[aqua]F8[-]:delete  [aqua]F2[-]:rename  [aqua]r[-]:rescan  [aqua]q[-]:quit"
}

func (a *App) highlightActive() {
	for _, p := range []*pane{a.left, a.right} {
		if p == a.active {
			p.table.SetBorderColor(tcell.ColorYellow)
		} else {
			p.table.SetBorderColor(tcell.ColorGray)
		}
	}
}

func (a *App) otherPane(p *pane) *pane {
	if p == a.left {
		return a.right
	}
	return a.left
}

func (a *App) globalKeys(ev *tcell.EventKey) *tcell.EventKey {
	mainActive := func() bool {
		name, _ := a.pages.GetFrontPage()
		return name == "main"
	}

	switch ev.Key() {
	case tcell.KeyLeft:
		if mainActive() {
			a.focusPane(a.left)
			return nil
		}
	case tcell.KeyRight:
		if mainActive() {
			a.focusPane(a.right)
			return nil
		}
	case tcell.KeyF2:
		a.rename()
		return nil
	case tcell.KeyF7:
		a.mkdir()
		return nil
	case tcell.KeyF8, tcell.KeyDelete:
		a.delete()
		return nil
	case tcell.KeyCtrlC:
		// Ctrl+Shift+C = copy (if the terminal forwards the Shift modifier);
		// plain Ctrl+C remains the quit shortcut.
		if ev.Modifiers()&tcell.ModShift != 0 {
			a.copyOrMove(false)
			return nil
		}
		a.tv.Stop()
		return nil
	case tcell.KeyCtrlM:
		// KeyCtrlM shares a byte with Enter (0x0D). Only treat as "move" when
		// the terminal signals Shift; otherwise let Enter bubble to the pane.
		if ev.Modifiers()&tcell.ModShift != 0 {
			a.copyOrMove(true)
			return nil
		}
	}
	switch ev.Rune() {
	case 'q':
		if mainActive() {
			a.tv.Stop()
			return nil
		}
	case 'r':
		if a.active == a.right && a.active.mode == modeHosts {
			a.scanHosts()
			return nil
		}
	case 'C':
		// Reliable copy shortcut for terminals that don't forward Ctrl+Shift.
		if mainActive() {
			a.copyOrMove(false)
			return nil
		}
	case 'M':
		if mainActive() {
			a.copyOrMove(true)
			return nil
		}
	}
	return ev
}

func (a *App) focusPane(p *pane) {
	if p == nil || a.active == p {
		return
	}
	a.active = p
	a.tv.SetFocus(p.table)
	a.highlightActive()
}

func (a *App) scanHosts() {
	a.status.SetText("[gray]scanning LAN for fcmd hosts...[-]")
	go func() {
		ctx := context.Background()
		hosts, err := discovery.Browse(ctx, 2500*time.Millisecond)
		a.tv.QueueUpdateDraw(func() {
			if err != nil {
				a.showError(fmt.Sprintf("mDNS browse: %v", err))
				return
			}
			a.right.hosts = hosts
			if a.right.mode == modeHosts {
				a.right.refresh()
			}
			a.status.SetText(fmt.Sprintf("[gray]found %d host(s). %s[-]", len(hosts), helpLine()))
		})
	}()
}

func (a *App) showError(msg string) {
	modal := tview.NewModal().
		SetText("[red]Error[-]\n\n" + msg).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			a.pages.RemovePage("error")
		})
	a.pages.AddPage("error", modal, true, true)
}

func (a *App) showInfo(msg string) {
	a.status.SetText("[green]" + msg + "[-]  " + helpLine())
}

func (a *App) prompt(label, initial string, done func(text string)) {
	form := tview.NewForm().
		AddInputField(label, initial, 60, nil, nil)
	form.SetBorder(true).SetTitle(label).SetTitleAlign(tview.AlignLeft)
	input := form.GetFormItem(0).(*tview.InputField)
	form.AddButton("OK", func() {
		text := input.GetText()
		a.pages.RemovePage("prompt")
		done(text)
	})
	form.AddButton("Cancel", func() {
		a.pages.RemovePage("prompt")
	})
	form.SetCancelFunc(func() { a.pages.RemovePage("prompt") })

	modal := centered(form, 70, 7)
	a.pages.AddPage("prompt", modal, true, true)
	a.tv.SetFocus(input)
}

func (a *App) confirm(msg string, ok func()) {
	modal := tview.NewModal().
		SetText(msg).
		AddButtons([]string{"OK", "Cancel"}).
		SetDoneFunc(func(_ int, label string) {
			a.pages.RemovePage("confirm")
			if label == "OK" {
				ok()
			}
		})
	a.pages.AddPage("confirm", modal, true, true)
}

func centered(inner tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(inner, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

// -------- file operations --------

func (a *App) mkdir() {
	p := a.active
	if p.mode != modeLocal && p.mode != modeRemote {
		return
	}
	a.prompt("New directory name", "", func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		full := p.fs.Join(p.cwd, name)
		if err := p.fs.Mkdir(full); err != nil {
			a.showError(err.Error())
			return
		}
		p.loadList()
	})
}

func (a *App) rename() {
	p := a.active
	if p.mode != modeLocal && p.mode != modeRemote {
		return
	}
	e, ok := p.currentEntry()
	if !ok || e.Name == ".." {
		return
	}
	a.prompt("Rename to", e.Name, func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == e.Name {
			return
		}
		oldPath := p.fs.Join(p.cwd, e.Name)
		newPath := p.fs.Join(p.cwd, name)
		if err := p.fs.Rename(oldPath, newPath); err != nil {
			a.showError(err.Error())
			return
		}
		p.loadList()
	})
}

func (a *App) delete() {
	p := a.active
	if p.mode != modeLocal && p.mode != modeRemote {
		return
	}
	paths := p.selectionPaths()
	if len(paths) == 0 {
		return
	}
	msg := fmt.Sprintf("Delete %d item(s)?\n\n", len(paths))
	for i, pth := range paths {
		if i >= 5 {
			msg += "..."
			break
		}
		msg += "- " + filepath.Base(pth) + "\n"
	}
	a.confirm(msg, func() {
		for _, pth := range paths {
			if err := p.fs.Delete(pth, true); err != nil {
				a.showError(err.Error())
				return
			}
		}
		p.selected = map[string]bool{}
		p.loadList()
	})
}

func (a *App) copyOrMove(move bool) {
	src := a.active
	dst := a.otherPane(src)
	if src.mode != modeLocal && src.mode != modeRemote {
		return
	}
	if dst.mode != modeLocal && dst.mode != modeRemote {
		a.showError("destination pane must be a browsable folder")
		return
	}
	paths := src.selectionPaths()
	if len(paths) == 0 {
		return
	}
	verb := "Copy"
	if move {
		verb = "Move"
	}
	a.confirm(fmt.Sprintf("%s %d item(s) to %s?", verb, len(paths), dst.cwd), func() {
		a.runTransfer(src, dst, paths, move)
	})
}

func (a *App) runTransfer(src, dst *pane, paths []string, move bool) {
	// Overall plan across all items.
	var totalFiles, totalBytes int64
	for _, pth := range paths {
		f, b, err := vfs.Plan(src.fs, pth)
		if err != nil {
			a.showError(fmt.Sprintf("plan %s: %v", pth, err))
			return
		}
		totalFiles += f
		totalBytes += b
	}

	// Progress modal.
	text := tview.NewTextView().SetDynamicColors(true)
	text.SetBorder(true).SetTitle(" Transfer ").SetTitleAlign(tview.AlignLeft)
	cancel := int32(0)
	text.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyEsc || ev.Rune() == 'q' {
			atomic.StoreInt32(&cancel, 1)
			return nil
		}
		return ev
	})
	modal := centered(text, 70, 9)
	a.pages.AddPage("progress", modal, true, true)
	a.tv.SetFocus(text)

	start := time.Now()
	var aggBytes, aggFiles int64

	update := func(bytesDone, totalB, filesDone, _ int64, name string, cur, curTotal int64) {
		// Running byte/file totals for this item only; we add by completion to aggregates.
		elapsed := time.Since(start).Seconds()
		if elapsed < 0.001 {
			elapsed = 0.001
		}
		speed := float64(aggBytes+bytesDone) / elapsed
		var overallPct, filePct int
		if totalBytes > 0 {
			overallPct = int(float64(aggBytes+bytesDone) * 100 / float64(totalBytes))
		}
		if curTotal > 0 {
			filePct = int(float64(cur) * 100 / float64(curTotal))
		}
		etaStr := "--"
		if speed > 0 && totalBytes > 0 {
			remaining := float64(totalBytes-(aggBytes+bytesDone)) / speed
			if remaining < 0 {
				remaining = 0
			}
			etaStr = fmt.Sprintf("%ds", int(remaining))
		}
		text.SetText(fmt.Sprintf(
			"Overall: %s %3d%%\nFile:    %s %3d%%\nFiles:   %d / %d\nCurrent: %s\nSpeed:   %s/s\nETA:     %s\n\n[gray]Esc to cancel[-]",
			bar(overallPct, 30), overallPct,
			bar(filePct, 30), filePct,
			aggFiles+filesDone, totalFiles,
			trimName(name, 60),
			humanSize(int64(speed)),
			etaStr,
		))
	}

	go func() {
		defer a.tv.QueueUpdateDraw(func() {
			a.pages.RemovePage("progress")
			a.tv.SetFocus(a.active.table)
			src.selected = map[string]bool{}
			src.loadList()
			dst.loadList()
		})

		for _, srcPath := range paths {
			if atomic.LoadInt32(&cancel) != 0 {
				a.tv.QueueUpdateDraw(func() { a.showInfo("cancelled") })
				return
			}
			name := filepath.Base(srcPath)
			dstPath := dst.fs.Join(dst.cwd, name)

			// Same-FS move: try rename first for atomicity.
			if move && sameFS(src, dst) {
				if err := src.fs.Rename(srcPath, dstPath); err == nil {
					f, b, _ := vfs.Plan(dst.fs, dstPath)
					aggFiles += f
					aggBytes += b
					a.tv.QueueUpdateDraw(func() {
						update(0, 0, 0, 0, name, b, b)
					})
					continue
				}
				// fall through to copy+delete if rename failed (e.g., cross-device)
			}

			f, b, err := vfs.Plan(src.fs, srcPath)
			if err != nil {
				a.tv.QueueUpdateDraw(func() { a.showError(err.Error()) })
				return
			}
			progressCB := func(bytesDone, tb, filesDone, _ int64, cname string, cur, curTotal int64) {
				if atomic.LoadInt32(&cancel) != 0 {
					return
				}
				a.tv.QueueUpdateDraw(func() { update(bytesDone, tb, filesDone, 0, cname, cur, curTotal) })
			}
			if err := vfs.Copy(src.fs, dst.fs, srcPath, dstPath, f, b, progressCB); err != nil {
				a.tv.QueueUpdateDraw(func() { a.showError(err.Error()) })
				return
			}
			aggFiles += f
			aggBytes += b

			if move {
				if err := src.fs.Delete(srcPath, true); err != nil {
					a.tv.QueueUpdateDraw(func() { a.showError(err.Error()) })
					return
				}
			}
		}
	}()
}

func sameFS(a, b *pane) bool {
	if a.fs == nil || b.fs == nil {
		return false
	}
	if a.fs.IsRemote() != b.fs.IsRemote() {
		return false
	}
	return a.fs.Label() == b.fs.Label()
}

func bar(pct, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < width; i++ {
		if i < filled {
			sb.WriteByte('#')
		} else {
			sb.WriteByte('-')
		}
	}
	sb.WriteByte(']')
	return sb.String()
}

func trimName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max+3:]
}

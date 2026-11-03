package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/tk-57/lol-settings/internal/lcu"
	"github.com/tk-57/lol-settings/internal/profile"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var user32 = syscall.NewLazyDLL("user32.dll")
var gdi32 = syscall.NewLazyDLL("gdi32.dll")
var comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
var sendMessage = user32.NewProc("SendMessageW")
var setWindowText = user32.NewProc("SetWindowTextW")

// application owns window state; completions marshal worker results onto its UI thread.
type application struct {
	hwnd, instance, font, linkFont, logEdit uintptr
	widgets                                 map[int]uintptr
	completions                             chan func()
	client                                  *lcu.Client
	account                                 lcu.Account
	logText                                 string
	busy, logExpanded                       bool
}

// Build-time flag used only for UI inspection without contacting a live client.
var previewMode = "false"

var version = "dev"

const (
	idConnect = 101 + iota
	idSave
	idApply
	idStatus
	idLogLink
)

func wide(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", "\uFFFD"))
	return p
}

func setText(h uintptr, s string) { setWindowText.Call(h, uintptr(unsafe.Pointer(wide(s)))) }

func (app *application) message(s, title string, flags uintptr) uintptr {
	r, _, _ := user32.NewProc("MessageBoxW").Call(app.hwnd, uintptr(unsafe.Pointer(wide(s))), uintptr(unsafe.Pointer(wide(title))), flags)
	return r
}

func (app *application) logLine(s string) {
	app.logText += time.Now().Format("15:04:05") + "  " + strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n") + "\r\n"
	const maxLogBytes = 64 * 1024
	if len(app.logText) > maxLogBytes {
		start := len(app.logText) - maxLogBytes
		if newline := strings.IndexByte(app.logText[start:], '\n'); newline >= 0 {
			app.logText = app.logText[start+newline+1:]
		}
	}
	if app.logEdit != 0 {
		setText(app.logEdit, app.logText)
		sendMessage.Call(app.logEdit, 0xB1, ^uintptr(0), ^uintptr(0))
		sendMessage.Call(app.logEdit, 0xB7, 0, 0)
	}
}

func (app *application) enabled(id int, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	user32.NewProc("EnableWindow").Call(app.widgets[id], v)
}

func (app *application) updateButtons() {
	app.enabled(idConnect, !app.busy)
	app.enabled(idSave, !app.busy && app.client != nil)
	app.enabled(idApply, !app.busy && app.client != nil)
	if previewMode == "true" {
		app.enabled(idConnect, false)
	}
}

func (app *application) background(work func() func()) {
	app.busy = true
	app.updateButtons()
	go func() {
		// work does not access window state; done runs in wndProc.
		done := work()
		app.completions <- func() { app.busy = false; done(); app.updateButtons() }
		user32.NewProc("PostMessageW").Call(app.hwnd, wmWorkComplete, 0, 0)
	}()
}

func (app *application) showError(err error) {
	app.logLine(err.Error())
	app.message(err.Error(), "操作できませんでした", 0x10)
}

func (app *application) reconnect() {
	setText(app.widgets[idStatus], "接続を確認しています…")
	app.background(func() func() {
		c, connectedAccount, err := lcu.Connect()
		return func() {
			if app.client != nil {
				app.client.Close()
			}
			app.client = c
			app.account = connectedAccount
			if err != nil {
				setText(app.widgets[idStatus], "未接続 — LoL にログインしてください")
				app.logLine(err.Error())
				return
			}
			setText(app.widgets[idStatus], "接続先: "+connectedAccount.Label())
			app.logLine("接続しました: " + connectedAccount.Label())
		}
	})
}

func (app *application) exportProfile() {
	path := app.dialog(true)
	if path == "" {
		return
	}
	c, selectedAccount := app.client, app.account
	app.background(func() func() {
		p, err := c.Snapshot(selectedAccount)
		if err == nil {
			err = profile.Save(path, p)
		}
		return func() {
			if err != nil {
				app.showError(err)
				return
			}
			app.logLine("保存しました: " + path)
			app.message("現在の設定を保存しました。\n別アカウントにログイン後、再接続してこのファイルを開いてください。", "保存完了", 0x40)
		}
	})
}

func (app *application) applyProfile() {
	path := app.dialog(false)
	if path == "" {
		return
	}
	p, err := profile.Load(path)
	if err != nil {
		app.showError(err)
		return
	}
	c, selectedAccount := app.client, app.account
	app.logLine(fmt.Sprintf("読み込み: %s\nゲーム: %d 項目 / キー: %d 項目", path, profile.Count(p.Game), profile.Count(p.Input)))
	app.background(func() func() {
		prepared, err := c.Prepare(selectedAccount, p)
		return func() {
			if err != nil {
				app.showError(err)
				return
			}
			plan := prepared.Plan
			if len(plan.Skipped) > 0 {
				app.logLine("現在のクライアントにない、または型が異なる項目を除外:\n" + strings.Join(plan.Skipped, "\n"))
			}
			if plan.Changed == 0 {
				app.message(fmt.Sprintf("変更する設定はありません。\n互換性がなく除外された項目: %d", len(plan.Skipped)), "適用内容", 0x40)
				return
			}
			prompt := fmt.Sprintf("適用先: %s\nファイル: %s\n\n変更: %d 項目\n未対応で除外: %d 項目\n\nこのアカウントへ適用しますか？", selectedAccount.Label(), filepath.Base(path), plan.Changed, len(plan.Skipped))
			if app.message(prompt, "設定の適用を確認", mbYesNo|mbIconQuestion|mbDefaultButton2) != idYes {
				return
			}
			app.background(func() func() {
				e := c.Apply(prepared)
				return func() {
					if e != nil {
						app.showError(e)
						return
					}
					app.logLine(fmt.Sprintf("%d 項目を適用し、読み戻し確認が完了しました。", plan.Changed))
					app.message("設定を適用しました。\n練習ツールなどで操作を確認してください。", "適用完了", 0x40)
				}
			})
		}
	})
}

func (app *application) child(class, text string, id, x, y, w, h int, style uintptr) uintptr {
	handle, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(wide(class))), uintptr(unsafe.Pointer(wide(text))), 0x40000000|0x10000000|style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), app.hwnd, uintptr(id), app.instance, 0)
	if id != 0 {
		app.widgets[id] = handle
	}
	sendMessage.Call(handle, 0x30, app.font, 1)
	return handle
}

func (app *application) command(id int) {
	if id == idLogLink {
		app.toggleLog()
		return
	}
	if app.busy {
		return
	}
	if previewMode == "true" && id == idConnect {
		return
	}
	if (id == idSave || id == idApply) && app.client == nil {
		return
	}
	switch id {
	case idConnect:
		app.reconnect()
	case idSave:
		app.exportProfile()
	case idApply:
		app.applyProfile()
	}
}

func (app *application) wndProc(h uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmSize:
		app.layoutLog()
		return 0
	case wmDrawItem:
		if w == idLogLink {
			type drawItem struct {
				ControlType, ControlID, ItemID, Action, State uint32
				Window, DC                                    uintptr
				Rect                                          [4]int32
				Data                                          uintptr
			}
			var d drawItem
			// WM_DRAWITEM supplies a native DRAWITEMSTRUCT valid for this callback.
			kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&d)), l, unsafe.Sizeof(d))
			brush, _, _ := user32.NewProc("GetSysColorBrush").Call(15)
			user32.NewProc("FillRect").Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), brush)
			old, _, _ := gdi32.NewProc("SelectObject").Call(d.DC, app.linkFont)
			gdi32.NewProc("SetBkMode").Call(d.DC, 1)
			gdi32.NewProc("SetTextColor").Call(d.DC, 0xB06020)
			user32.NewProc("DrawTextW").Call(d.DC, uintptr(unsafe.Pointer(wide(app.logLinkText()))), ^uintptr(0), uintptr(unsafe.Pointer(&d.Rect)), 0x24)
			gdi32.NewProc("SelectObject").Call(d.DC, old)
			if d.State&0x10 != 0 {
				user32.NewProc("DrawFocusRect").Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)))
			}
			return 1
		}
	case wmSetCursor:
		if w == app.widgets[idLogLink] {
			cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32649)
			user32.NewProc("SetCursor").Call(cursor)
			return 1
		}
	case wmCommand:
		app.command(int(w & 0xffff))
		return 0
	case wmWorkComplete:
		select {
		case done := <-app.completions:
			done()
		default:
		}
		return 0
	case wmClose:
		if app.busy {
			app.message("処理中です。完了してから閉じてください。", "LoL Settings", 0x40)
			return 0
		}
		user32.NewProc("DestroyWindow").Call(h)
		return 0
	case wmDestroy:
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	r, _, _ := user32.NewProc("DefWindowProcW").Call(h, uintptr(msg), w, l)
	return r
}

func (app *application) run() {
	runtime.LockOSThread()
	// Let Windows scale the fixed logical layout. No browser/runtime is needed.
	app.instance, _, _ = kernel32.NewProc("GetModuleHandleW").Call(0)
	app.font, _, _ = gdi32.NewProc("CreateFontW").Call(16, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wide("Yu Gothic UI"))))
	defer gdi32.NewProc("DeleteObject").Call(app.font)
	app.linkFont, _, _ = gdi32.NewProc("CreateFontW").Call(15, 0, 0, 0, 400, 0, 1, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wide("Yu Gothic UI"))))
	defer gdi32.NewProc("DeleteObject").Call(app.linkFont)
	type wndClass struct {
		Size, Style                        uint32
		Proc                               uintptr
		ClsExtra, WndExtra                 int32
		Instance, Icon, Cursor, Background uintptr
		Menu, Class                        *uint16
		IconSmall                          uintptr
	}
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	wc := wndClass{Proc: syscall.NewCallback(app.wndProc), Instance: app.instance, Cursor: cursor, Background: 16, Class: wide("LoLSettingsNativeWindow")}
	wc.Size = uint32(unsafe.Sizeof(wc))
	registered, _, _ := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
	if registered == 0 {
		app.message("ウィンドウを初期化できません。", "起動エラー", 0x10)
		return
	}
	app.hwnd, _, _ = user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(wc.Class)), uintptr(unsafe.Pointer(wide("LoL Settings "+version))), 0x00CA0000, 0x80000000, 0x80000000, 336, collapsedHeight, 0, 0, app.instance, 0)
	if app.hwnd == 0 {
		app.message("ウィンドウを作成できません。", "起動エラー", 0x10)
		return
	}
	app.child("STATIC", "未接続", idStatus, 12, 12, 216, 20, 0x4000)
	app.child("BUTTON", "再接続", idConnect, 238, 8, 70, 26, 0x10000)
	app.child("BUTTON", "現在の設定を保存…", idSave, 12, 44, 156, 27, 0x10000)
	app.child("BUTTON", "設定を適用…", idApply, 176, 44, 132, 27, 0x10000)
	app.child("BUTTON", "ログを開く", idLogLink, 12, 81, 80, 20, 0x1000B)
	app.updateButtons()
	user32.NewProc("ShowWindow").Call(app.hwnd, 1)
	user32.NewProc("UpdateWindow").Call(app.hwnd)
	app.logLine("LoL Settings " + version)
	if previewMode == "true" {
		app.logLine("画面検証モード: LCU への接続は行いません。")
		app.enabled(idConnect, false)
	} else {
		app.reconnect()
	}
	type msgStruct struct {
		Hwnd           uintptr
		Message        uint32
		WParam, LParam uintptr
		Time           uint32
		X, Y           int32
		Private        uint32
	}
	var msg msgStruct
	for {
		r, _, _ := user32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) == -1 {
			app.showError(fmt.Errorf("ウィンドウメッセージの取得に失敗しました"))
			break
		}
		if r == 0 {
			break
		}
		d, _, _ := user32.NewProc("IsDialogMessageW").Call(app.hwnd, uintptr(unsafe.Pointer(&msg)))
		if d == 0 {
			user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
			user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
	if app.client != nil {
		app.client.Close()
	}
}

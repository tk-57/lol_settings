package main

import (
	"fmt"
	"unsafe"
)

const (
	collapsedHeight = 144
	expandedHeight  = 364
	logTop          = 109
	logMargin       = 12
)

func (app *application) logLinkText() string {
	if app.logExpanded {
		return "ログを閉じる"
	}
	return "ログを開く"
}

func (app *application) layoutLog() {
	if app.hwnd == 0 || app.logEdit == 0 || !app.logExpanded {
		return
	}
	var rect [4]int32
	user32.NewProc("GetClientRect").Call(app.hwnd, uintptr(unsafe.Pointer(&rect)))
	width, height := rect[2]-2*logMargin, rect[3]-logTop-logMargin
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	user32.NewProc("MoveWindow").Call(app.logEdit, logMargin, logTop, uintptr(width), uintptr(height), 1)
}

func (app *application) resizeForLog() error {
	var rect [4]int32
	if ok, _, _ := user32.NewProc("GetWindowRect").Call(app.hwnd, uintptr(unsafe.Pointer(&rect))); ok == 0 {
		return fmt.Errorf("ウィンドウサイズを取得できません")
	}
	x, y, width := rect[0], rect[1], rect[2]-rect[0]
	height := int32(collapsedHeight)
	if app.logExpanded {
		height = expandedHeight
	}
	// Keep an expanded panel inside the monitor's work area near screen edges.
	monitor, _, _ := user32.NewProc("MonitorFromWindow").Call(app.hwnd, 2)
	var info struct {
		Size          uint32
		Monitor, Work [4]int32
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); ok != 0 {
		if available := info.Work[3] - info.Work[1]; height > available {
			height = available
		}
		if y+height > info.Work[3] {
			y = info.Work[3] - height
		}
		if y < info.Work[1] {
			y = info.Work[1]
		}
	}
	if ok, _, _ := user32.NewProc("SetWindowPos").Call(app.hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0x14); ok == 0 {
		return fmt.Errorf("ウィンドウサイズを変更できません")
	}
	return nil
}

func (app *application) toggleLog() {
	if app.logEdit == 0 {
		// A read-only multiline edit, initially hidden and owned by the main window.
		app.logEdit, _, _ = user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(wide("EDIT"))), 0, 0x40000000|0x00800000|0x00200000|0x0004|0x0040|0x0800|0x10000, logMargin, logTop, 456, 208, app.hwnd, 108, app.instance, 0)
		if app.logEdit == 0 {
			app.showError(fmt.Errorf("ログの表示欄を作成できません"))
			return
		}
		sendMessage.Call(app.logEdit, 0x30, app.font, 1)
		sendMessage.Call(app.logEdit, 0xC5, 1024*1024, 0)
		setText(app.logEdit, app.logText)
	}
	previous := app.logExpanded
	app.logExpanded = !app.logExpanded
	if !app.logExpanded {
		user32.NewProc("SetFocus").Call(app.widgets[idLogLink])
		user32.NewProc("ShowWindow").Call(app.logEdit, 0)
	}
	if err := app.resizeForLog(); err != nil {
		app.logExpanded = previous
		if previous {
			user32.NewProc("ShowWindow").Call(app.logEdit, 5)
		}
		app.showError(err)
		return
	}
	if app.logExpanded {
		app.layoutLog()
		user32.NewProc("ShowWindow").Call(app.logEdit, 5)
	}
	setText(app.widgets[idLogLink], app.logLinkText())
	user32.NewProc("InvalidateRect").Call(app.widgets[idLogLink], 0, 1)
}

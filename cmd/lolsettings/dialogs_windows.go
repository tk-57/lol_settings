package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefExt                       *uint16
	CustomData, Hook             uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	Reserved2, FlagsEx           uint32
}

func (app *application) dialog(save bool) string {
	var buf [32768]uint16
	filter := syscall.StringToUTF16("JSON files (*.json)")
	filter = append(filter, syscall.StringToUTF16("*.json")...)
	filter = append(filter, 0)
	title := "適用する設定ファイルを選択"
	if save {
		copy(buf[:], syscall.StringToUTF16("settings.json"))
		title = "現在の設定を保存"
	}
	o := openFileName{Owner: app.hwnd, Filter: &filter[0], FilterIndex: 1, File: &buf[0], MaxFile: uint32(len(buf)), Title: wide(title), Flags: 0x80000 | 0x800 | 0x8, DefExt: wide("json")}
	o.Size = uint32(unsafe.Sizeof(o))
	proc := comdlg32.NewProc("GetOpenFileNameW")
	if save {
		o.Flags |= 2
		proc = comdlg32.NewProc("GetSaveFileNameW")
	} else {
		o.Flags |= 0x1000
	}
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&o)))
	if r == 0 {
		if code, _, _ := comdlg32.NewProc("CommDlgExtendedError").Call(); code != 0 {
			app.showError(fmt.Errorf("ファイル選択画面を開けませんでした (0x%X)", code))
		}
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

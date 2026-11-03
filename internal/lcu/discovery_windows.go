package lcu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

func processDirectories() []string {
	snapshot := kernel32.NewProc("CreateToolhelp32Snapshot")
	first := kernel32.NewProc("Process32FirstW")
	next := kernel32.NewProc("Process32NextW")
	open := kernel32.NewProc("OpenProcess")
	query := kernel32.NewProc("QueryFullProcessImageNameW")
	closeHandle := kernel32.NewProc("CloseHandle")
	type processEntry struct {
		Size, Usage, PID        uint32
		Heap                    uintptr
		Module, Threads, Parent uint32
		Priority                int32
		Flags                   uint32
		Exe                     [260]uint16
	}
	h, _, _ := snapshot.Call(2, 0)
	if h == ^uintptr(0) {
		return nil
	}
	defer closeHandle.Call(h)
	e := processEntry{}
	e.Size = uint32(unsafe.Sizeof(e))
	var dirs []string
	ok, _, _ := first.Call(h, uintptr(unsafe.Pointer(&e)))
	for ok != 0 {
		name := syscall.UTF16ToString(e.Exe[:])
		if strings.EqualFold(name, "LeagueClientUx.exe") || strings.EqualFold(name, "LeagueClient.exe") {
			ph, _, _ := open.Call(0x1000, 0, uintptr(e.PID))
			if ph != 0 {
				var buf [32768]uint16
				size := uint32(len(buf))
				r, _, _ := query.Call(ph, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
				closeHandle.Call(ph)
				if r != 0 {
					dirs = append(dirs, filepath.Dir(syscall.UTF16ToString(buf[:size])))
				}
			}
		}
		ok, _, _ = next.Call(h, uintptr(unsafe.Pointer(&e)))
	}
	return dirs
}

func candidates() []string {
	dirs := processDirectories()
	// Riot's installation index supports custom drives without scanning user files.
	b, err := os.ReadFile(filepath.Join(os.Getenv("PROGRAMDATA"), "Riot Games", "RiotClientInstalls.json"))
	if err == nil {
		var data struct {
			Associated map[string]string `json:"associated_client"`
		}
		if json.Unmarshal(b, &data) == nil {
			for dir := range data.Associated {
				if strings.Contains(strings.ToLower(dir), "league of legends") {
					dirs = append(dirs, dir)
				}
			}
		}
	}
	dirs = append(dirs, `C:\Riot Games\League of Legends`, filepath.Join(os.Getenv("ProgramFiles"), "Riot Games", "League of Legends"))
	seen := map[string]bool{}
	var paths []string
	for _, dir := range dirs {
		p := filepath.Join(dir, "lockfile")
		key := strings.ToLower(p)
		if !seen[key] {
			seen[key] = true
			paths = append(paths, p)
		}
	}
	return paths
}

// Connect discovers local installations and rejects ambiguous active clients.
func Connect() (*Client, Account, error) {
	var selected *Client
	var account Account
	var lastErr error
	for _, path := range candidates() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		c, err := newClient(path)
		if err != nil {
			lastErr = err
			continue
		}
		a, err := c.Account()
		if err != nil {
			c.Close()
			lastErr = err
			continue
		}
		if selected != nil {
			selected.Close()
			c.Close()
			return nil, Account{}, fmt.Errorf("複数の LoL クライアントを検出しました。利用するクライアントだけを起動して再接続してください")
		}
		selected, account = c, a
	}
	if selected != nil {
		return selected, account, nil
	}
	if lastErr != nil {
		return nil, Account{}, lastErr
	}
	return nil, Account{}, fmt.Errorf("LoL が見つかりません。League of Legends にログイン後、「再接続」を押してください")
}

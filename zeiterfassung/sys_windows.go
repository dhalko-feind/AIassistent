//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	procMessageBox = user32.NewProc("MessageBoxW")
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procDriveType  = kernel32.NewProc("GetDriveTypeW")
)

const driveFixed = 3

func showMessage(title, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(title)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x40) // MB_ICONINFORMATION
}

func openBrowser(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// openFolder öffnet den Ordner im Explorer. Der Explorer meldet auch bei Erfolg
// einen Rückgabewert ungleich 0, deshalb wird nur der Start geprüft.
func openFolder(path string) error {
	cmd := exec.Command("explorer", path)
	return cmd.Start()
}

// fixedDrives liefert die lokalen Festplattenlaufwerke (kein Netz, kein Wechselmedium),
// damit die Suche nicht an getrennten Netzlaufwerken hängt.
func fixedDrives() []string {
	var out []string
	for c := 'C'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		p, _ := syscall.UTF16PtrFromString(root)
		done := make(chan uintptr, 1)
		go func() {
			r, _, _ := procDriveType.Call(uintptr(unsafe.Pointer(p)))
			done <- r
		}()
		select {
		case r := <-done:
			if r == driveFixed {
				if _, err := os.Stat(root); err == nil {
					out = append(out, root)
				}
			}
		case <-time.After(2 * time.Second):
		}
	}
	return out
}

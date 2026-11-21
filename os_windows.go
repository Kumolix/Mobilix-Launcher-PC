//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// hideWindow запускает дочерний процесс без консольного окна.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

type memStatus struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// totalRAMMB возвращает объём оперативной памяти в мегабайтах (0, если не удалось узнать).
func totalRAMMB() int {
	k := syscall.NewLazyDLL("kernel32.dll")
	p := k.NewProc("GlobalMemoryStatusEx")
	var m memStatus
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := p.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0
	}
	return int(m.TotalPhys / (1024 * 1024))
}

func openPath(p string) {
	c := exec.Command("explorer.exe", p)
	_ = c.Start()
}

func openURL(u string) {
	c := exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	hideWindow(c)
	_ = c.Start()
}

func killTree(pid int) {
	c := exec.Command("taskkill", "/PID", itoa(pid), "/T", "/F")
	hideWindow(c)
	_ = c.Run()
}

//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Заглушки для сборки и проверки на Linux/macOS (основная цель — Windows).

func hideWindow(c *exec.Cmd) {}

func totalRAMMB() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "MemTotal:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				return n / 1024
			}
		}
	}
	return 0
}

func openPath(p string) { _ = exec.Command("xdg-open", p).Start() }

func openURL(u string) { _ = exec.Command("xdg-open", u).Start() }

func killTree(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// На Linux своё окно не встроено: используется окно браузера в режиме приложения.
func (a *App) runNative(url string) bool { return false }

func focusWindow() bool { return false }

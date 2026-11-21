//go:build windows

package main

import (
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

const winTitle = "Mobilix"

// runNative открывает собственное окно с WebView2 (как у Modrinth App) и блокируется до закрытия окна.
// Возвращает false, если WebView2 недоступен: тогда лаунчер откроется в окне браузера.
func (a *App) runNative(url string) bool {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  a.path("webview2"),
		WindowOptions: webview2.WindowOptions{
			Title:  winTitle,
			Width:  1180,
			Height: 700,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		a.lg.add("[Mobilix] WebView2 not available, using browser window")
		return false
	}
	defer w.Destroy()
	w.SetSize(1180, 700, webview2.HintNone)
	w.Navigate(url)
	w.Run()
	return true
}

// focusWindow выводит на передний план уже открытое окно Mobilix.
func focusWindow() bool {
	u := syscall.NewLazyDLL("user32.dll")
	find := u.NewProc("FindWindowW")
	title, _ := syscall.UTF16PtrFromString(winTitle)
	hwnd, _, _ := find.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return false
	}
	u.NewProc("ShowWindow").Call(hwnd, 9) // SW_RESTORE
	u.NewProc("SetForegroundWindow").Call(hwnd)
	return true
}

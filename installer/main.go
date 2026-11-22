//go:build windows && installer

// Установщик Mobilix Launcher: копирует лаунчер в Program Files, создаёт ярлыки и запись об удалении.
// Сборка: скопировать Mobilix-Launcher.exe в installer/payload.exe и собрать с тегом installer.
package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

//go:embed payload.exe
var payload []byte

const (
	appName = "Mobilix Launcher"
	exeName = "Mobilix-Launcher.exe"
	regKey  = `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\MobilixLauncher`
)

var user32 = syscall.NewLazyDLL("user32.dll")

func box(text string, flags uintptr) int {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(appName)
	r, _, _ := user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
	return int(r)
}

const (
	mbOK       = 0x0
	mbYesNo    = 0x4
	mbIconInfo = 0x40
	mbIconQ    = 0x20
	mbIconErr  = 0x10
	idYes      = 6
)

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c.Run()
}

func ps(script string) error {
	return run("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
}

func installDir() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, appName)
}

func main() {
	dir := installDir()
	if len(os.Args) > 1 && strings.EqualFold(os.Args[1], "/uninstall") {
		uninstall(dir)
		return
	}
	if box("Установить "+appName+" в папку:\n"+dir+"\n\nБудут созданы ярлыки на рабочем столе и в меню «Пуск».", mbYesNo|mbIconQ) != idYes {
		return
	}
	_ = run("taskkill", "/IM", exeName, "/F")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		box("Не удалось создать папку: "+err.Error(), mbIconErr)
		return
	}
	exe := filepath.Join(dir, exeName)
	_ = os.Remove(exe + ".old")
	if err := os.WriteFile(exe, payload, 0o755); err != nil {
		box("Не удалось записать файл: "+err.Error(), mbIconErr)
		return
	}
	// чтобы лаунчер мог обновлять сам себя без прав администратора
	_ = run("icacls", dir, "/grant", "*S-1-5-32-545:(OI)(CI)M", "/T", "/C", "/Q")
	self, _ := os.Executable()
	if b, err := os.ReadFile(self); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "Uninstall.exe"), b, 0o755)
	}
	ps(`$s=New-Object -ComObject WScript.Shell;` +
		`foreach($p in @([Environment]::GetFolderPath('CommonDesktopDirectory'),[Environment]::GetFolderPath('CommonPrograms'))){` +
		`$l=$s.CreateShortcut((Join-Path $p '` + appName + `.lnk'));$l.TargetPath='` + exe + `';$l.WorkingDirectory='` + dir + `';$l.IconLocation='` + exe + `,0';$l.Save()}`)
	for _, kv := range [][]string{
		{"DisplayName", appName}, {"DisplayIcon", exe}, {"Publisher", "Kumolix"}, {"InstallLocation", dir},
		{"UninstallString", `"` + filepath.Join(dir, "Uninstall.exe") + `" /uninstall`},
		{"URLInfoAbout", "https://github.com/Kumolix/Mobilix-Launcher-PC"},
	} {
		_ = run("reg", "add", regKey, "/v", kv[0], "/t", "REG_SZ", "/d", kv[1], "/f")
	}
	_ = run("reg", "add", regKey, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f")
	if box("Готово! Запустить "+appName+" сейчас?", mbYesNo|mbIconInfo) == idYes {
		_ = exec.Command("explorer.exe", exe).Start() // от имени обычного пользователя
	}
}

func uninstall(dir string) {
	if box("Удалить "+appName+"?\nМиры, моды и настройки в папке данных (%APPDATA%\\Mobilix) останутся.", mbYesNo|mbIconQ) != idYes {
		return
	}
	_ = run("taskkill", "/IM", exeName, "/F")
	ps(`foreach($p in @([Environment]::GetFolderPath('CommonDesktopDirectory'),[Environment]::GetFolderPath('CommonPrograms'))){Remove-Item (Join-Path $p '` + appName + `.lnk') -ErrorAction SilentlyContinue}`)
	_ = run("reg", "delete", regKey, "/f")
	_ = os.Remove(filepath.Join(dir, exeName))
	_ = os.Remove(filepath.Join(dir, exeName+".old"))
	// сам Uninstall.exe и папка удаляются отложенно, после выхода
	_ = exec.Command("cmd", "/c", "ping 127.0.0.1 -n 3 >nul & rmdir /s /q \""+dir+"\"").Start()
	box("Лаунчер удалён.", mbOK|mbIconInfo)
}

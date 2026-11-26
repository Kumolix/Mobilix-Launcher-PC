//go:build windows && installer

// Установщик Mobilix Launcher: копирует лаунчер в Program Files, создаёт ярлыки и запись об удалении.
// Сборка: скопировать Mobilix-Launcher.exe в installer/payload.exe и собрать с тегом installer.
package main

import (
	"embed"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

//go:embed ui/index.html
var uiHTML string

//go:embed ui/icon.png
var uiIcon []byte

//go:embed payload.exe
var payload []byte

// assets: в сборке на GitHub сюда кладётся официальный zerotier.msi (в локальной сборке его может не быть — тогда скачивается).
//
//go:embed all:assets
var assets embed.FS

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

func zeroTierPresent() bool {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	_, err := os.Stat(filepath.Join(pd, "ZeroTier", "One"))
	return err == nil
}

// installLauncher копирует лаунчер, создаёт ярлыки и запись об удалении.
func installLauncher(dir string, step func(int, string)) error {
	step(5, "Закрываю запущенный лаунчер…")
	_ = run("taskkill", "/IM", exeName, "/F")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку: %w", err)
	}
	exe := filepath.Join(dir, exeName)
	_ = os.Remove(exe + ".old")
	step(15, "Копирую файлы лаунчера…")
	if err := os.WriteFile(exe, payload, 0o755); err != nil {
		return fmt.Errorf("не удалось записать файл: %w", err)
	}
	// чтобы лаунчер мог обновлять сам себя без прав администратора
	_ = run("icacls", dir, "/grant", "*S-1-5-32-545:(OI)(CI)M", "/T", "/C", "/Q")
	self, _ := os.Executable()
	if b, err := os.ReadFile(self); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "Uninstall.exe"), b, 0o755)
	}
	step(35, "Создаю ярлыки…")
	ps(`$s=New-Object -ComObject WScript.Shell;` +
		`foreach($p in @([Environment]::GetFolderPath('CommonDesktopDirectory'),[Environment]::GetFolderPath('CommonPrograms'))){` +
		`$l=$s.CreateShortcut((Join-Path $p '` + appName + `.lnk'));$l.TargetPath='` + exe + `';$l.WorkingDirectory='` + dir + `';$l.IconLocation='` + exe + `,0';$l.Save()}`)
	step(45, "Регистрирую в «Установке и удалении программ»…")
	for _, kv := range [][]string{
		{"DisplayName", appName}, {"DisplayIcon", exe}, {"Publisher", "Kumolix"}, {"InstallLocation", dir},
		{"UninstallString", `"` + filepath.Join(dir, "Uninstall.exe") + `" /uninstall`},
		{"URLInfoAbout", "https://github.com/Kumolix/Mobilix-Launcher-PC"},
	} {
		_ = run("reg", "add", regKey, "/v", kv[0], "/t", "REG_SZ", "/d", kv[1], "/f")
	}
	_ = run("reg", "add", regKey, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f")
	return nil
}

// installZeroTier ставит ZeroTier One (MSI вшит в установщик, иначе скачивается с zerotier.com). Тихо: установщик уже с правами администратора.
func installZeroTier() error {
	var err error
	if msi, e := assets.ReadFile("assets/zerotier.msi"); e == nil && len(msi) > 1<<20 {
		f := filepath.Join(os.TempDir(), "ZeroTierOne.msi")
		if err = os.WriteFile(f, msi, 0o644); err == nil {
			cmd := exec.Command("msiexec", "/i", f, "/qn", "/norestart")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			err = cmd.Run()
			os.Remove(f)
		}
	} else {
		err = ps(`$ErrorActionPreference='Stop';[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12;` +
			`$f=Join-Path $env:TEMP 'ZeroTierOne.msi';Invoke-WebRequest -UseBasicParsing 'https://download.zerotier.com/dist/ZeroTier%20One.msi' -OutFile $f;` +
			`$p=Start-Process msiexec -ArgumentList '/i',$f,'/qn','/norestart' -Wait -PassThru;Remove-Item $f -ErrorAction SilentlyContinue;exit $p.ExitCode`)
	}
	return err
}

func main() {
	dir := installDir()
	if len(os.Args) > 1 && strings.EqualFold(os.Args[1], "/uninstall") {
		uninstall(dir)
		return
	}
	exe := filepath.Join(dir, exeName)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus:     true,
		DataPath:      filepath.Join(os.TempDir(), "mobilix-setup-wv2"),
		WindowOptions: webview2.WindowOptions{Title: "Установка " + appName, Width: 760, Height: 520, IconId: 1, Center: true},
	})
	if w == nil {
		legacy(dir)
		return
	}
	defer w.Destroy()
	w.SetSize(760, 520, webview2.HintFixed)
	step := func(p int, m string) {
		w.Dispatch(func() { w.Eval(fmt.Sprintf("progress(%d,%q)", p, m)) })
	}
	w.Bind("goInfo", func() map[string]any {
		_, upd := os.Stat(exe)
		return map[string]any{"dir": dir, "zt": zeroTierPresent(), "upd": upd == nil}
	})
	w.Bind("goClose", func() { w.Dispatch(func() { w.Terminate() }) })
	w.Bind("goLaunch", func() {
		_ = exec.Command("explorer.exe", exe).Start() // от имени обычного пользователя
		w.Dispatch(func() { w.Terminate() })
	})
	w.Bind("goInstall", func(launcher, zt bool) {
		go func() {
			var errs []string
			if launcher {
				if err := installLauncher(dir, step); err != nil {
					errs = append(errs, err.Error())
					launcher = false
				}
			}
			if zt {
				step(55, "Устанавливаю ZeroTier One…")
				if err := installZeroTier(); err != nil {
					errs = append(errs, "ZeroTier One не установился — его можно поставить позже на странице «Игра по сети» в лаунчере.")
				}
			}
			step(100, "Готово")
			msg := "Всё установлено."
			if len(errs) > 0 {
				msg = strings.Join(errs, " ")
			}
			ok := len(errs) == 0
			w.Dispatch(func() { w.Eval(fmt.Sprintf("finish(%t,%q,%t)", ok, msg, launcher)) })
		}()
	})
	html := strings.Replace(uiHTML, "__ICON__", "data:image/png;base64,"+base64.StdEncoding.EncodeToString(uiIcon), 1)
	w.SetHtml(html)
	w.Run()
}

// legacy — запасной вариант без WebView2: диалоговые окна Windows.
func legacy(dir string) {
	if box("Установить "+appName+" в папку:\n"+dir+"\n\nБудут созданы ярлыки на рабочем столе и в меню «Пуск».", mbYesNo|mbIconQ) != idYes {
		return
	}
	if err := installLauncher(dir, func(int, string) {}); err != nil {
		box(err.Error(), mbIconErr)
		return
	}
	if !zeroTierPresent() && box("Установить ZeroTier One?\n\nОн нужен для игры по сети с друзьями.", mbYesNo|mbIconQ) == idYes {
		if installZeroTier() != nil {
			box("ZeroTier One не установился. Его можно поставить позже в лаунчере.", mbIconErr)
		}
	}
	if box("Готово! Запустить "+appName+" сейчас?", mbYesNo|mbIconInfo) == idYes {
		_ = exec.Command("explorer.exe", filepath.Join(dir, exeName)).Start()
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

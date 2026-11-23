package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const updRepo = "Kumolix/Mobilix-Launcher-PC"

type relInfo struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func updAsset(r relInfo) string {
	for _, x := range r.Assets {
		n := strings.ToLower(x.Name)
		if strings.Contains(n, "setup") || strings.Contains(n, "install") {
			continue
		}
		if runtime.GOOS == "windows" && strings.HasSuffix(n, ".exe") {
			return x.URL
		}
		if runtime.GOOS != "windows" && strings.Contains(n, "linux") {
			return x.URL
		}
	}
	return ""
}

func (a *App) latestRelease(ctx context.Context) (relInfo, error) {
	var r relInfo
	err := getJSONNoMirror(ctx, "https://api.github.com/repos/"+updRepo+"/releases/latest", &r)
	return r, err
}

// verParts разбирает «v1.2.3» / «1.2.3» в числа; префиксы вроде «PC-» отбрасываются.
func verParts(v string) []int {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "0123456789"); i > 0 {
		v = v[i:]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func verNewer(remote, cur string) bool {
	r, c := verParts(remote), verParts(cur)
	if r == nil || c == nil {
		return false
	}
	for i := 0; i < len(r) || i < len(c); i++ {
		var x, y int
		if i < len(r) {
			x = r[i]
		}
		if i < len(c) {
			y = c[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// checkUpdate сообщает true, когда проверка завершена (даже если обновления нет), и false при сетевой ошибке — тогда её повторят позже.
func (a *App) checkUpdate() bool {
	if a.settings().UpdMode == 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := a.latestRelease(ctx)
	if err != nil {
		return false
	}
	if r.Draft || r.Prerelease || r.Tag == "" || !verNewer(r.Tag, appVersion) {
		return true
	}
	a.mu.Lock()
	a.updateTag = r.Tag
	a.mu.Unlock()
	return true
}

func cleanOld() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// applyUpdate скачивает новый exe, подменяет текущий (старый переименовывается — это можно и у запущенного) и перезапускает лаунчер.
func (a *App) applyUpdate(ctx context.Context) error {
	r, err := a.latestRelease(ctx)
	if err != nil {
		return err
	}
	u := updAsset(r)
	if u == "" {
		return errors.New(a.tr("В релизе нет файла для этой системы", "The release has no file for this system"))
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	a.setStage(a.tr("Обновление лаунчера", "Updating the launcher"), 0, 0)
	nw := exe + ".new"
	if err := a.fetchFile(ctx, u, nw, "", 0, nil); err != nil {
		return err
	}
	_ = os.Remove(exe + ".old")
	if err := os.Rename(exe, exe+".old"); err != nil {
		_ = os.Remove(nw)
		return errors.New(a.tr("Нет прав на замену файла. Скачайте релиз вручную.", "No permission to replace the file. Download the release manually."))
	}
	if err := os.Rename(nw, exe); err != nil {
		_ = os.Rename(exe+".old", exe)
		return err
	}
	_ = os.Chmod(exe, 0o755)
	_ = os.Remove(a.path("run.json"))
	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	a.shutdown()
	return nil
}

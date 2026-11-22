package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const updRepo = "Kumolix/Mobilix-Launcher-PC"

type relInfo struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
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

func (a *App) checkUpdate() {
	if a.settings().UpdMode == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := a.latestRelease(ctx)
	if err != nil || r.Tag == "" || strings.TrimPrefix(r.Tag, "v") == appVersion || updAsset(r) == "" {
		return
	}
	a.mu.Lock()
	a.updateTag = r.Tag
	a.mu.Unlock()
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

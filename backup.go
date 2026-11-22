package main

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupKeep = 3

func (a *App) backupDir(key string) string {
	if key == "" {
		key = "game"
	}
	return a.path("backups", key)
}

func changedSince(dir string, t time.Time) bool {
	changed := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || changed {
			return filepath.SkipAll
		}
		if !d.IsDir() {
			if info, e := d.Info(); e == nil && info.ModTime().After(t) {
				changed = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return changed
}

func zipDir(zw *zip.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Dir(root), p)
		w, err := zw.Create(filepath.ToSlash(filepath.Join(prefix, rel)))
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return nil // файл занят игрой: пропускаем
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

// backupWorlds: после закрытия игры сохраняет zip только с теми мирами, где что-то менялось; хранит последние backupKeep копий.
func (a *App) backupWorlds(gdir, key string, since time.Time) {
	saves := filepath.Join(gdir, "saves")
	ents, _ := os.ReadDir(saves)
	var worlds []string
	for _, e := range ents {
		if e.IsDir() && changedSince(filepath.Join(saves, e.Name()), since.Add(-2*time.Second)) {
			worlds = append(worlds, e.Name())
		}
	}
	if len(worlds) == 0 {
		return
	}
	dir := a.backupDir(key)
	_ = os.MkdirAll(dir, 0o755)
	name := filepath.Join(dir, time.Now().Format("20060102-150405")+".zip")
	f, err := os.Create(name)
	if err != nil {
		return
	}
	zw := zip.NewWriter(f)
	for _, w := range worlds {
		_ = zipDir(zw, filepath.Join(saves, w), "")
	}
	_ = zw.Close()
	_ = f.Close()
	a.lg.add(fmt.Sprintf("[Mobilix] "+a.tr("Копия миров сохранена: %s (%d)", "World backup saved: %s (%d)"), filepath.Base(name), len(worlds)))
	files, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	sort.Strings(files)
	for len(files) > backupKeep {
		_ = os.Remove(files[0])
		files = files[1:]
	}
}

type backupInfo struct {
	Key    string   `json:"key"`
	File   string   `json:"file"`
	Time   string   `json:"time"`
	Size   int64    `json:"size"`
	Worlds []string `json:"worlds"`
}

func (a *App) listBackups() []backupInfo {
	var out []backupInfo
	keys, _ := os.ReadDir(a.path("backups"))
	for _, k := range keys {
		if !k.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(a.path("backups", k.Name()), "*.zip"))
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
		for _, f := range files {
			st, err := os.Stat(f)
			if err != nil {
				continue
			}
			zr, err := zip.OpenReader(f)
			if err != nil {
				continue
			}
			seen := map[string]bool{}
			var ws []string
			for _, e := range zr.File {
				if w := strings.SplitN(e.Name, "/", 2)[0]; w != "" && !seen[w] {
					seen[w] = true
					ws = append(ws, w)
				}
			}
			zr.Close()
			sort.Strings(ws)
			out = append(out, backupInfo{k.Name(), filepath.Base(f), st.ModTime().Format("2006-01-02 15:04"), st.Size(), ws})
		}
	}
	return out
}

// restoreWorld: распаковывает один мир из копии; существующий мир не затирается, а переименовывается.
func (a *App) restoreWorld(key, file, world string) error {
	if file != filepath.Base(file) || key != filepath.Base(key) || world != filepath.Base(world) || world == "" {
		return fmt.Errorf("bad request")
	}
	s := a.settings()
	var gdir string
	if strings.HasPrefix(key, "build-") || key == "game" {
		gdir = a.path("instances", key)
		if key == "game" {
			gdir = a.path("game")
		}
	} else {
		gdir = a.path("instances", key)
	}
	_ = s
	saves := filepath.Join(gdir, "saves")
	target := filepath.Join(saves, world)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, target+"-before-restore-"+time.Now().Format("150405")); err != nil {
			return err
		}
	}
	zr, err := zip.OpenReader(filepath.Join(a.backupDir(key), file))
	if err != nil {
		return err
	}
	defer zr.Close()
	root := filepath.Clean(saves) + string(os.PathSeparator)
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, world+"/") || f.FileInfo().IsDir() {
			continue
		}
		out := filepath.Join(saves, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(out, root) {
			continue
		}
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		w, err := os.Create(out)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(w, rc)
		w.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) hBackups(w http.ResponseWriter, r *http.Request) {
	jw(w, map[string]any{"backups": a.listBackups()})
}

func (a *App) hRestore(w http.ResponseWriter, r *http.Request) {
	var b struct{ Key, File, World string }
	if readBody(r, &b) != nil {
		jerr(w, 400, "bad json")
		return
	}
	a.gmu.Lock()
	running := a.gameCmd != nil
	a.gmu.Unlock()
	if running {
		jerr(w, 409, a.tr("Сначала закройте игру", "Close the game first"))
		return
	}
	if err := a.restoreWorld(b.Key, b.File, b.World); err != nil {
		jerr(w, 500, err.Error())
		return
	}
	jw(w, map[string]any{"ok": true})
}

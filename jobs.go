package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
)

// Eff — фактические параметры установки: версия игры, загрузчик и ключ папки.
// У каждой связки «версия + загрузчик» и у каждой сборки своя папка с модами и мирами.
type Eff struct{ MC, Loader, LoaderVer, Key string }

func (s Settings) eff() Eff {
	if s.Build != "" {
		for _, b := range s.Builds {
			if b.ID == s.Build {
				return Eff{b.MC, b.Loader, b.LoaderVer, "build-" + b.ID}
			}
		}
	}
	key := s.Selected
	if s.Loader != "" && s.Loader != "vanilla" {
		key = s.Loader + "-" + s.Selected
	}
	if !s.Isolate || s.Selected == "" {
		key = ""
	}
	return Eff{s.Selected, s.Loader, s.LoaderVer, key}
}

func (a *App) gameDirE(e Eff) string {
	if e.Key == "" {
		return a.path("game")
	}
	return a.path("instances", e.Key)
}

func (a *App) gameDir(s Settings) string { return a.gameDirE(s.eff()) }

func (e Eff) title() string {
	if e.MC == "" {
		return ""
	}
	if e.Loader == "" || e.Loader == "vanilla" {
		return e.MC
	}
	return e.MC + " • " + loaderLabel(e.Loader)
}

func loaderLabel(l string) string {
	switch l {
	case "fabric":
		return "Fabric"
	case "quilt":
		return "Quilt"
	case "forge":
		return "Forge"
	case "neoforge":
		return "NeoForge"
	}
	return "Vanilla"
}

// startFn запускает фоновую задачу (скачивание, запуск, импорт, обновление); одновременно идёт только одна.
func (a *App) startFn(kind string, fn func(ctx context.Context) error) error {
	a.jmu.Lock()
	if a.busy {
		a.jmu.Unlock()
		return errors.New(a.tr("Уже идёт загрузка", "A download is already running"))
	}
	a.gmu.Lock()
	running := a.gameCmd != nil
	a.gmu.Unlock()
	if kind == "play" && running {
		a.jmu.Unlock()
		return errors.New(a.tr("Игра уже запущена", "The game is already running"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.busy, a.kind, a.errMsg, a.stage, a.done, a.total, a.cancel = true, kind, "", "", 0, 0, cancel
	a.jmu.Unlock()
	go func() {
		err := fn(ctx)
		a.jmu.Lock()
		a.busy = false
		a.cancel = nil
		a.stage, a.done, a.total = "", 0, 0
		if err != nil {
			if errors.Is(err, context.Canceled) {
				a.errMsg = a.tr("Отменено", "Cancelled")
			} else {
				a.errMsg = err.Error()
			}
		}
		a.jmu.Unlock()
		if err != nil {
			a.lg.add("[Mobilix] " + a.tr("Ошибка: ", "Error: ") + err.Error())
		}
		cancel()
	}()
	return nil
}

func (a *App) startJob(kind string) error {
	return a.startFn(kind, func(ctx context.Context) error { return a.runJob(ctx, kind) })
}

func (a *App) runJob(ctx context.Context, kind string) error {
	s := a.settings()
	e := s.eff()
	if e.MC == "" {
		return errors.New(a.tr("Выберите версию Minecraft", "Pick a Minecraft version"))
	}
	if kind == "play" {
		a.rotateLog()
	}
	prof, err := a.ensureProfile(ctx, e)
	if err != nil {
		return err
	}
	v, err := a.loadVersion(prof)
	if err != nil {
		return err
	}
	if err := a.ensureFiles(ctx, v, s); err != nil {
		return err
	}
	if kind == "install" {
		a.lg.add("[Mobilix] " + a.tr("Версия скачана: ", "Version downloaded: ") + prof)
		return nil
	}
	javaMajor := 8
	if v.JavaVersion != nil {
		javaMajor = v.JavaVersion.MajorVersion
	}
	java, err := a.ensureJava(ctx, javaMajor, s)
	if err != nil {
		return err
	}
	return a.launch(ctx, v, s, e, java)
}

// installedList перечисляет установки: версия, загрузчик, скачана ли она.
func (a *App) installedList() []map[string]any {
	var out []map[string]any
	for _, id := range a.installedVersions() {
		mc, loader := id, "vanilla"
		if v, err := a.rawProfile(id); err == nil && v.InheritsFrom != "" {
			mc = v.InheritsFrom
			loader = loaderOfProfile(id)
		}
		out = append(out, map[string]any{"id": id, "mc": mc, "loader": loader})
	}
	return out
}

func loaderOfProfile(id string) string {
	l := strings.ToLower(id)
	switch {
	case strings.HasPrefix(l, "neoforge-") || strings.Contains(l, "-neoforge-"):
		return "neoforge"
	case strings.HasPrefix(l, "quilt-loader-"):
		return "quilt"
	case strings.Contains(l, "forge"):
		return "forge"
	case strings.HasPrefix(l, "fabric-loader-"):
		return "fabric"
	}
	return "vanilla"
}

func (a *App) hLoaders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60e9)
	defer cancel()
	list, err := a.loaderVersions(ctx, r.URL.Query().Get("loader"), r.URL.Query().Get("mc"))
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	jw(w, map[string]any{"versions": list})
}

func profilePath(a *App, id string) string {
	return filepath.Join(a.path("shared", "versions", id), id+".json")
}

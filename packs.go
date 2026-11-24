package main

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// safeJoin: путь внутри root, иначе пустая строка (защита от zip-slip и ../).
func safeJoin(root, rel string) string {
	rel = filepath.FromSlash(strings.TrimLeft(rel, "/\\"))
	p := filepath.Join(root, rel)
	if p != filepath.Clean(root) && !strings.HasPrefix(p, filepath.Clean(root)+string(os.PathSeparator)) {
		return ""
	}
	return p
}

// extractPrefix распаковывает записи архива с префиксом prefix в dest (без префикса).
func extractPrefix(zr *zip.ReadCloser, prefix, dest string) error {
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		out := safeJoin(dest, strings.TrimPrefix(f.Name, prefix))
		if out == "" {
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

func zipFile(zr *zip.ReadCloser, name string) []byte {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			b, _ := io.ReadAll(io.LimitReader(rc, 32<<20))
			return b
		}
	}
	return nil
}

// ---------- создание сборки ----------

func (a *App) addBuild(name, mc, loader, lver string) Build {
	if name == "" {
		name = mc
	}
	if loader == "" {
		loader = "vanilla"
	}
	b := Build{ID: newID(), Name: name, MC: mc, Loader: loader, LoaderVer: lver}
	a.saveSettings(func(s *Settings) { s.Builds = append(s.Builds, b) })
	_ = os.MkdirAll(a.path("instances", "build-"+b.ID), 0o755)
	return b
}

func (a *App) buildDir(id string) string { return a.path("instances", "build-"+id) }

// finishPack: сборка выбирается и скачивается (профиль, библиотеки, загрузчик).
func (a *App) finishPack(ctx context.Context, b Build, icon string) error {
	if icon != "" {
		_ = a.fetchFile(ctx, icon, filepath.Join(a.buildDir(b.ID), "mobilix-icon.png"), "", 0, nil)
	}
	a.saveSettings(func(s *Settings) { s.Build = b.ID })
	a.lg.add("[Mobilix] " + a.tr("Сборка создана: ", "Build created: ") + b.Name)
	return a.runJob(ctx, "install")
}

// ---------- Modrinth .mrpack ----------

type mrIndex struct {
	Name         string            `json:"name"`
	Dependencies map[string]string `json:"dependencies"`
	Files        []struct {
		Path      string            `json:"path"`
		Hashes    map[string]string `json:"hashes"`
		Downloads []string          `json:"downloads"`
		Env       map[string]string `json:"env"`
	} `json:"files"`
}

func loaderFromDeps(d map[string]string) (string, string) {
	for _, l := range []string{"fabric-loader", "quilt-loader", "neoforge", "forge"} {
		if v, ok := d[l]; ok {
			return strings.TrimSuffix(l, "-loader"), v
		}
	}
	return "vanilla", ""
}

func (a *App) installMrpack(ctx context.Context, path, nameHint, icon string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	var idx mrIndex
	if json.Unmarshal(zipFile(zr, "modrinth.index.json"), &idx) != nil || idx.Dependencies["minecraft"] == "" {
		return errors.New(a.tr("Это не сборка Modrinth", "Not a Modrinth pack"))
	}
	loader, lver := loaderFromDeps(idx.Dependencies)
	name := idx.Name
	if name == "" {
		name = nameHint
	}
	b := a.addBuild(name, idx.Dependencies["minecraft"], loader, lver)
	gdir := a.buildDir(b.ID)
	var items []dlItem
	for _, f := range idx.Files {
		if f.Env["client"] == "unsupported" || len(f.Downloads) == 0 {
			continue
		}
		dest := safeJoin(gdir, f.Path)
		if dest == "" {
			continue
		}
		items = append(items, dlItem{f.Downloads[0], dest, strings.ToLower(f.Hashes["sha1"]), 0})
	}
	if err := a.fetchAll(ctx, a.tr("Файлы сборки", "Pack files"), items, a.settings().Threads); err != nil {
		return err
	}
	for _, pre := range []string{"overrides/", "client-overrides/"} {
		if err := extractPrefix(zr, pre, gdir); err != nil {
			return err
		}
	}
	return a.finishPack(ctx, b, icon)
}

// ---------- CurseForge zip ----------

type cfManifest struct {
	Name      string `json:"name"`
	Overrides string `json:"overrides"`
	MC        struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	Files []struct {
		Project int `json:"projectID"`
		File    int `json:"fileID"`
	} `json:"files"`
}

func (a *App) installCFPack(ctx context.Context, path, nameHint, icon string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	var m cfManifest
	if json.Unmarshal(zipFile(zr, "manifest.json"), &m) != nil || m.MC.Version == "" {
		return errors.New(a.tr("Не удалось прочитать сборку CurseForge", "Cannot read the CurseForge pack"))
	}
	loader, lver := "vanilla", ""
	for _, l := range m.MC.ModLoaders {
		if i := strings.Index(l.ID, "-"); i > 0 {
			loader, lver = l.ID[:i], l.ID[i+1:]
			if l.Primary {
				break
			}
		}
	}
	name := m.Name
	if name == "" {
		name = nameHint
	}
	b := a.addBuild(name, m.MC.Version, loader, lver)
	gdir := a.buildDir(b.ID)
	if len(m.Files) > 0 {
		var fids, pids []int
		for _, f := range m.Files {
			fids = append(fids, f.File)
			pids = append(pids, f.Project)
		}
		var files struct {
			Data []cfFile `json:"data"`
		}
		if err := cfDo(ctx, "POST", "/mods/files", map[string]any{"fileIds": fids}, &files); err != nil {
			return err
		}
		var mods struct {
			Data []struct {
				ID      int `json:"id"`
				ClassID int `json:"classId"`
			} `json:"data"`
		}
		_ = cfDo(ctx, "POST", "/mods", map[string]any{"modIds": pids}, &mods)
		class := map[int]int{}
		for _, x := range mods.Data {
			class[x.ID] = x.ClassID
		}
		var items []dlItem
		for _, f := range files.Data {
			dir := "mods"
			switch class[f.ModID] {
			case 12:
				dir = "resourcepacks"
			case 6552:
				dir = "shaderpacks"
			}
			items = append(items, dlItem{f.url(), filepath.Join(gdir, dir, filepath.Base(f.FileName)), f.sha1(), 0})
		}
		if err := a.fetchAll(ctx, a.tr("Файлы сборки", "Pack files"), items, a.settings().Threads); err != nil {
			return err
		}
	}
	ov := m.Overrides
	if ov == "" {
		ov = "overrides"
	}
	if err := extractPrefix(zr, strings.TrimSuffix(ov, "/")+"/", gdir); err != nil {
		return err
	}
	return a.finishPack(ctx, b, icon)
}

// ---------- сборка Mobilix ----------

type mobilixPack struct {
	Name      string `json:"name"`
	MC        string `json:"mc"`
	Loader    string `json:"loader"`
	LoaderVer string `json:"loaderVer"`
}

func (a *App) installMobilixPack(ctx context.Context, path, nameHint, icon string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	var m mobilixPack
	if json.Unmarshal(zipFile(zr, "mobilix-build.json"), &m) != nil || m.MC == "" {
		return errors.New("bad build")
	}
	if m.Name == "" {
		m.Name = nameHint
	}
	b := a.addBuild(m.Name, m.MC, m.Loader, m.LoaderVer)
	if err := extractPrefix(zr, "game/", a.buildDir(b.ID)); err != nil {
		return err
	}
	return a.finishPack(ctx, b, icon)
}

// installPackFile определяет формат по содержимому архива.
func (a *App) installPackFile(ctx context.Context, path, nameHint, icon string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return errors.New(a.tr("Это не архив сборки", "Not a pack archive"))
	}
	has := func(n string) bool {
		for _, f := range zr.File {
			if f.Name == n {
				return true
			}
		}
		return false
	}
	mr, cf, mx := has("modrinth.index.json"), has("manifest.json"), has("mobilix-build.json")
	zr.Close()
	switch {
	case mx:
		return a.installMobilixPack(ctx, path, nameHint, icon)
	case mr:
		return a.installMrpack(ctx, path, nameHint, icon)
	case cf:
		return a.installCFPack(ctx, path, nameHint, icon)
	}
	return errors.New(a.tr("Неизвестный формат сборки", "Unknown pack format"))
}

// installModpack: сборка из каталога (Modrinth или CurseForge).
func (a *App) installModpack(ctx context.Context, src, id, name, icon string) error {
	s := a.settings()
	f, _, err := a.pickFile(ctx, s, Eff{}, src, id, "modpack")
	if err != nil {
		return err
	}
	tmp := a.path("cache", "pack-"+newID()+".zip")
	defer os.Remove(tmp)
	a.setStage(a.tr("Скачивание сборки", "Downloading the pack"), 0, 0)
	if err := a.fetchFile(ctx, f.url, tmp, f.sha1, 0, nil); err != nil {
		return err
	}
	return a.installPackFile(ctx, tmp, name, icon)
}

// ---------- экспорт ----------

func (a *App) exportBuild(w http.ResponseWriter, b Build) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+regexp.MustCompile(`[^\w.\- ]`).ReplaceAllString(b.Name, "_")+`.mobilix.zip"`)
	zw := zip.NewWriter(w)
	meta, _ := json.Marshal(mobilixPack{b.Name, b.MC, b.Loader, b.LoaderVer})
	mw, _ := zw.Create("mobilix-build.json")
	_, _ = mw.Write(meta)
	gdir := a.buildDir(b.ID)
	for _, d := range []string{"mods", "config", "resourcepacks", "shaderpacks"} {
		_ = zipDir(zw, filepath.Join(gdir, d), "game")
	}
	if bb, err := os.ReadFile(filepath.Join(gdir, "options.txt")); err == nil {
		ow, _ := zw.Create("game/options.txt")
		_, _ = ow.Write(bb)
	}
	_ = zw.Close()
}

// ---------- HTTP ----------

func (a *App) hBuildCreate(w http.ResponseWriter, r *http.Request) {
	var b struct{ Name, MC, Loader, LoaderVer string }
	if readBody(r, &b) != nil || b.MC == "" {
		jerr(w, 400, a.tr("Выберите версию", "Pick a version"))
		return
	}
	nb := a.addBuild(strings.TrimSpace(b.Name), b.MC, b.Loader, b.LoaderVer)
	jw(w, map[string]any{"build": nb})
}

func (a *App) findBuild(id string) (Build, bool) {
	for _, b := range a.settings().Builds {
		if b.ID == id {
			return b, true
		}
	}
	return Build{}, false
}

func (a *App) hBuildDelete(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID string }
	if readBody(r, &b) != nil || b.ID == "" || b.ID != filepath.Base(b.ID) {
		jerr(w, 400, "bad request")
		return
	}
	a.gmu.Lock()
	running := a.gameCmd != nil
	a.gmu.Unlock()
	if running {
		jerr(w, 409, a.tr("Сначала закройте игру", "Close the game first"))
		return
	}
	a.saveSettings(func(s *Settings) {
		var keep []Build
		for _, x := range s.Builds {
			if x.ID != b.ID {
				keep = append(keep, x)
			}
		}
		s.Builds = keep
		if s.Build == b.ID {
			s.Build = ""
		}
	})
	_ = os.RemoveAll(a.buildDir(b.ID))
	jw(w, map[string]any{"ok": true})
}

func (a *App) hBuildDup(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID string }
	if readBody(r, &b) != nil {
		jerr(w, 400, "bad request")
		return
	}
	src, ok := a.findBuild(b.ID)
	if !ok {
		jerr(w, 404, "not found")
		return
	}
	nb := a.addBuild(src.Name+" (copy)", src.MC, src.Loader, src.LoaderVer)
	if err := copyTree(a.buildDir(src.ID), a.buildDir(nb.ID)); err != nil {
		jerr(w, 500, err.Error())
		return
	}
	jw(w, map[string]any{"build": nb})
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(t, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer in.Close()
		out, err := os.Create(t)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func (a *App) hBuildExport(w http.ResponseWriter, r *http.Request) {
	b, ok := a.findBuild(r.URL.Query().Get("id"))
	if !ok {
		jerr(w, 404, "not found")
		return
	}
	a.exportBuild(w, b)
}

// hImport принимает файл в теле запроса: сборка (zip), мод, ресурспак, шейдер, датапак или мир.
func (a *App) hImport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ, name := q.Get("type"), filepath.Base(q.Get("name"))
	if name == "." || name == string(os.PathSeparator) || name == "" {
		name = "file"
	}
	tmp := a.path("cache", "import-"+newID())
	_ = os.MkdirAll(a.path("cache"), 0o755)
	f, err := os.Create(tmp)
	if err != nil {
		jerr(w, 500, err.Error())
		return
	}
	_, err = io.Copy(f, r.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		jerr(w, 400, err.Error())
		return
	}
	if typ == "pack" {
		nm := strings.TrimSuffix(strings.TrimSuffix(name, ".zip"), ".mrpack")
		err := a.startFn("pack", func(ctx context.Context) error {
			defer os.Remove(tmp)
			return a.installPackFile(ctx, tmp, nm, "")
		})
		if err != nil {
			os.Remove(tmp)
			jerr(w, 409, err.Error())
			return
		}
		jw(w, map[string]any{"job": true})
		return
	}
	defer os.Remove(tmp)
	e := a.settings().effFor(q.Get("build"))
	gdir := a.gameDirE(e)
	if typ == "world" {
		zr, err := zip.OpenReader(tmp)
		if err != nil {
			jerr(w, 400, a.tr("Нужен zip с миром", "A world zip is required"))
			return
		}
		defer zr.Close()
		wn := strings.TrimSuffix(name, filepath.Ext(name))
		prefix := ""
		for _, zf := range zr.File { // мир может лежать в подпапке архива
			if strings.HasSuffix(zf.Name, "level.dat") {
				prefix = strings.TrimSuffix(zf.Name, "level.dat")
				break
			}
		}
		dest := filepath.Join(gdir, "saves", wn)
		for i := 2; ; i++ {
			if _, err := os.Stat(dest); err != nil {
				break
			}
			dest = filepath.Join(gdir, "saves", wn+" ("+strconv.Itoa(i)+")")
		}
		if err := extractPrefix(zr, prefix, dest); err != nil {
			jerr(w, 500, err.Error())
			return
		}
		jw(w, map[string]any{"ok": true})
		return
	}
	dir, ok := typeDirs[typ]
	if !ok || dir == "" {
		jerr(w, 400, "bad type")
		return
	}
	if typ == "datapack" {
		wd := filepath.Base(q.Get("world"))
		if wd == "." || wd == "" {
			jerr(w, 400, a.tr("Выберите мир", "Pick a world"))
			return
		}
		dir = "saves/" + wd + "/datapacks"
	}
	dest := filepath.Join(gdir, filepath.FromSlash(dir), name)
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	if err := os.Rename(tmp, dest); err != nil {
		if err := copyTree2(tmp, dest); err != nil {
			jerr(w, 500, err.Error())
			return
		}
	}
	jw(w, map[string]any{"ok": true})
}

func copyTree2(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

var _ = fmt.Sprint
var _ = url.QueryEscape

// иконка сборки: из каталога или выбранная вручную
func (a *App) hBuildIcon(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(r.URL.Query().Get("id"))
	f := filepath.Join(a.buildDir(id), "mobilix-icon.png")
	if r.Method == "POST" {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if len(b) < 16 {
			jerr(w, 400, "bad image")
			return
		}
		if err := os.WriteFile(f, b, 0o644); err != nil {
			jerr(w, 500, err.Error())
			return
		}
		jw(w, map[string]any{"ok": true})
		return
	}
	b, err := os.ReadFile(f)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}

// hBuildFromCurrent: «Сохранить как сборку» — всё скачанное в выбранной установке копируется в новую сборку под ту же версию.
func (a *App) hBuildFromCurrent(w http.ResponseWriter, r *http.Request) {
	var b struct{ Name, Build string }
	if readBody(r, &b) != nil {
		jerr(w, 400, "bad request")
		return
	}
	s := a.settings()
	e := s.effFor(b.Build)
	if e.MC == "" {
		jerr(w, 400, a.tr("Сначала выберите версию игры", "Pick a game version first"))
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = instName(e) + " " + a.tr("(сохранено)", "(saved)")
	}
	src := a.gameDirE(e)
	nb := a.addBuild(name, e.MC, e.Loader, e.LoaderVer)
	dst := a.buildDir(nb.ID)
	for _, d := range []string{"mods", "resourcepacks", "shaderpacks", "config", "datapacks"} {
		if _, err := os.Stat(filepath.Join(src, d)); err == nil {
			if err := copyTree(filepath.Join(src, d), filepath.Join(dst, d)); err != nil {
				jerr(w, 500, err.Error())
				return
			}
		}
	}
	for _, f := range []string{"mobilix-index.json", "options.txt", "servers.dat"} {
		if data, err := os.ReadFile(filepath.Join(src, f)); err == nil {
			_ = os.WriteFile(filepath.Join(dst, f), data, 0o644)
		}
	}
	a.lg.add("[Mobilix] " + a.tr("Сборка сохранена: ", "Build saved: ") + name)
	jw(w, map[string]any{"build": nb})
}

func instName(e Eff) string {
	if e.Loader == "" || e.Loader == "vanilla" {
		return e.MC
	}
	return e.MC + " " + e.Loader
}

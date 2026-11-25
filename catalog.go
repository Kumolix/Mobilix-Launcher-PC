package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const modrinthOfficial = "https://api.modrinth.com/v2"

// cfKey подставляется при сборке: -ldflags "-X main.cfKey=...". В репозитории ключа нет.
var cfKey = ""

var typeDirs = map[string]string{"mod": "mods", "resourcepack": "resourcepacks", "shader": "shaderpacks", "datapack": "datapacks", "modpack": ""}

func modrinthBases(s Settings) []string {
	var out []string
	if s.Mirrors && s.MirrorAPI != "" {
		out = append(out, strings.TrimRight(s.MirrorAPI, "/"))
	}
	return append(out, modrinthOfficial)
}

var (
	mrCacheMu sync.Mutex
	mrCache   = map[string]mrCached{}
)

type mrCached struct {
	at time.Time
	b  []byte
}

// mrGet опрашивает зеркало и официальный API одновременно и берёт первый успешный ответ; ответы кэшируются на 5 минут.
func mrGet(ctx context.Context, s Settings, path string, v any) error {
	bases := modrinthBases(s)
	mrCacheMu.Lock()
	if c, ok := mrCache[path]; ok && time.Since(c.at) < 5*time.Minute {
		mrCacheMu.Unlock()
		return json.Unmarshal(c.b, v)
	}
	mrCacheMu.Unlock()
	rctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	type res struct {
		b   []byte
		err error
	}
	ch := make(chan res, len(bases))
	for _, b := range bases {
		go func(b string) {
			resp, err := httpGet(rctx, b+path)
			if err != nil {
				ch <- res{nil, err}
				return
			}
			defer resp.Body.Close()
			bb, err := io.ReadAll(resp.Body)
			ch <- res{bb, err}
		}(b)
	}
	var last error
	for range bases {
		r := <-ch
		if r.err == nil && json.Valid(r.b) {
			mrCacheMu.Lock()
			if len(mrCache) > 200 {
				mrCache = map[string]mrCached{}
			}
			mrCache[path] = mrCached{time.Now(), r.b}
			mrCacheMu.Unlock()
			return json.Unmarshal(r.b, v)
		}
		if r.err != nil {
			last = r.err
		}
	}
	if last == nil {
		last = errors.New("bad response")
	}
	return last
}

func getJSONNoMirror(ctx context.Context, u string, v any) error {
	resp, err := httpGet(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// ---------- индекс установленного ----------
// В папке каждой установки лежит mobilix-index.json: что и откуда поставлено. По нему каталог помнит «Установлено».

type idxEntry struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	World string   `json:"world,omitempty"`
	Files []string `json:"files"` // пути относительно папки игры
}

var idxMu sync.Mutex

func idxFile(gdir string) string { return filepath.Join(gdir, "mobilix-index.json") }

func readIdx(gdir string) map[string]idxEntry {
	m := map[string]idxEntry{}
	if b, err := os.ReadFile(idxFile(gdir)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (a *App) idxUpdate(gdir string, mut func(map[string]idxEntry)) {
	idxMu.Lock()
	defer idxMu.Unlock()
	m := readIdx(gdir)
	mut(m)
	for k, e := range m { // убираем записи, файлы которых удалены вручную
		alive := false
		for _, f := range e.Files {
			if _, err := os.Stat(filepath.Join(gdir, filepath.FromSlash(f))); err == nil {
				alive = true
			}
		}
		if !alive {
			delete(m, k)
		}
	}
	b, _ := json.MarshalIndent(m, "", " ")
	_ = os.MkdirAll(gdir, 0o755)
	_ = os.WriteFile(idxFile(gdir), b, 0o644)
}

func idxKey(src, typ, id, world string) string {
	k := src + ":" + typ + ":" + id
	if world != "" {
		k += ":" + world
	}
	return k
}

// ---------- загрузчики в фильтрах ----------

func mrLoaders(l string) []string {
	switch l {
	case "fabric":
		return []string{"fabric"}
	case "quilt":
		return []string{"quilt", "fabric"}
	case "forge":
		return []string{"forge"}
	case "neoforge":
		return []string{"neoforge"}
	}
	return nil
}

func cfLoader(l string) int {
	switch l {
	case "forge":
		return 1
	case "fabric", "quilt":
		return 4
	case "neoforge":
		return 6
	}
	return 0
}

var cfClass = map[string]int{"mod": 6, "modpack": 4471, "resourcepack": 12, "shader": 6552, "datapack": 6945}

type hit struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Author      string `json:"author"`
	Downloads   int    `json:"downloads"`
	Installed   bool   `json:"installed"`
	Installing  bool   `json:"installing"`
}

// ---------- CurseForge ----------

func cfDo(ctx context.Context, method, path string, body any, v any) error {
	if cfKey == "" {
		return errors.New("CurseForge: no API key in this build")
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.curseforge.com/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", cfKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mobilix-PC/"+appVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("CurseForge HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type cfFile struct {
	ID          int    `json:"id"`
	ModID       int    `json:"modId"`
	FileName    string `json:"fileName"`
	DownloadURL string `json:"downloadUrl"`
	Hashes      []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	Dependencies []struct {
		ModID int `json:"modId"`
		Rel   int `json:"relationType"`
	} `json:"dependencies"`
}

func (f cfFile) url() string {
	if f.DownloadURL != "" {
		return f.DownloadURL
	}
	return fmt.Sprintf("https://edge.forgecdn.net/files/%d/%d/%s", f.ID/1000, f.ID%1000, url.PathEscape(f.FileName))
}

func (f cfFile) sha1() string {
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			return strings.ToLower(h.Value)
		}
	}
	return ""
}

// ---------- поиск ----------

func (a *App) hCatSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ := q.Get("type")
	if _, ok := typeDirs[typ]; !ok {
		typ = "mod"
	}
	src := q.Get("src")
	s := a.settings()
	if src == "" {
		src = s.ModSource
	}
	e := s.effFor(q.Get("build"))
	off, _ := strconv.Atoi(q.Get("offset"))
	filter := q.Get("filter") == "1" && e.MC != ""
	idx := readIdx(a.gameDirE(e))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var hits []hit
	total := 0
	if src == "curseforge" {
		v := url.Values{}
		v.Set("gameId", "432")
		v.Set("classId", strconv.Itoa(cfClass[typ]))
		v.Set("searchFilter", q.Get("q"))
		v.Set("pageSize", "20")
		v.Set("index", strconv.Itoa(off))
		if q.Get("q") == "" {
			v.Set("sortField", "2") // без запроса — по популярности
			v.Set("sortOrder", "desc")
		}
		if filter {
			v.Set("gameVersion", e.MC)
			if m := cfLoader(e.Loader); m > 0 && typ == "mod" {
				v.Set("modLoaderType", strconv.Itoa(m))
			}
		}
		var res struct {
			Data []struct {
				ID        int     `json:"id"`
				Name      string  `json:"name"`
				Summary   string  `json:"summary"`
				Downloads float64 `json:"downloadCount"`
				Logo      struct {
					ThumbnailURL string `json:"thumbnailUrl"`
				} `json:"logo"`
				Authors []struct {
					Name string `json:"name"`
				} `json:"authors"`
			} `json:"data"`
			Pagination struct {
				Total int `json:"totalCount"`
			} `json:"pagination"`
		}
		if err := cfDo(ctx, "GET", "/mods/search?"+v.Encode(), nil, &res); err != nil {
			jerr(w, 502, a.tr("CurseForge недоступен: ", "CurseForge unavailable: ")+err.Error())
			return
		}
		for _, d := range res.Data {
			h := hit{ID: strconv.Itoa(d.ID), Title: d.Name, Description: d.Summary, Icon: d.Logo.ThumbnailURL, Downloads: int(d.Downloads)}
			if len(d.Authors) > 0 {
				h.Author = d.Authors[0].Name
			}
			hits = append(hits, h)
		}
		total = res.Pagination.Total
	} else {
		facets := [][]string{{"project_type:" + typ}}
		if filter {
			facets = append(facets, []string{"versions:" + e.MC})
			if typ == "mod" {
				var cats []string
				for _, l := range mrLoaders(e.Loader) {
					cats = append(cats, "categories:"+l)
				}
				if len(cats) > 0 {
					facets = append(facets, cats)
				}
			}
		}
		fb, _ := json.Marshal(facets)
		var res struct {
			Hits []struct {
				ProjectID   string `json:"project_id"`
				Title       string `json:"title"`
				Description string `json:"description"`
				IconURL     string `json:"icon_url"`
				Author      string `json:"author"`
				Downloads   int    `json:"downloads"`
			} `json:"hits"`
			Total int `json:"total_hits"`
		}
		index := "relevance"
		if q.Get("q") == "" {
			index = "downloads"
		}
		p := fmt.Sprintf("/search?query=%s&facets=%s&limit=20&offset=%d&index=%s", url.QueryEscape(q.Get("q")), url.QueryEscape(string(fb)), off, index)
		if err := mrGet(ctx, s, p, &res); err != nil {
			jerr(w, 502, a.tr("Каталог недоступен: ", "Catalog unavailable: ")+err.Error())
			return
		}
		for _, d := range res.Hits {
			hits = append(hits, hit{d.ProjectID, d.Title, d.Description, d.IconURL, d.Author, d.Downloads, false, false})
		}
		total = res.Total
	}
	catJobs.Lock()
	for i := range hits {
		if catJobs.m[jobKey(src, typ, hits[i].ID, "")] == "busy" {
			hits[i].Installing = true
		}
	}
	catJobs.Unlock()
	if typ == "modpack" {
		for i := range hits {
			if a.packHave(jobKey(src, typ, hits[i].ID, "")) {
				hits[i].Installed = true
			}
		}
	}
	for i := range hits {
		for k := range idx {
			if strings.HasPrefix(k, idxKey(src, typ, hits[i].ID, "")) {
				hits[i].Installed = true
			}
		}
	}
	jw(w, map[string]any{"hits": hits, "total": total, "cf": cfKey != ""})
}

// ---------- установка ----------

type fileRef struct{ url, name, sha1 string }

// pickFile выбирает файл проекта под версию и загрузчик, а также его обязательные зависимости.
func (a *App) pickFile(ctx context.Context, s Settings, e Eff, src, id, typ string) (fileRef, []string, error) {
	if src == "curseforge" {
		v := url.Values{}
		v.Set("pageSize", "1")
		if typ != "modpack" {
			v.Set("gameVersion", e.MC)
		}
		if m := cfLoader(e.Loader); m > 0 && typ == "mod" {
			v.Set("modLoaderType", strconv.Itoa(m))
		}
		var res struct {
			Data []cfFile `json:"data"`
		}
		if err := cfDo(ctx, "GET", "/mods/"+id+"/files?"+v.Encode(), nil, &res); err != nil {
			return fileRef{}, nil, err
		}
		if len(res.Data) == 0 {
			return fileRef{}, nil, errors.New(a.tr("Нет файла для версии ", "No file for version ") + e.MC)
		}
		f := res.Data[0]
		var deps []string
		for _, d := range f.Dependencies {
			if d.Rel == 3 {
				deps = append(deps, strconv.Itoa(d.ModID))
			}
		}
		return fileRef{f.url(), f.FileName, f.sha1()}, deps, nil
	}
	q := url.Values{}
	if typ != "modpack" {
		b, _ := json.Marshal([]string{e.MC})
		q.Set("game_versions", string(b))
	}
	if ls := mrLoaders(e.Loader); len(ls) > 0 && typ == "mod" {
		b, _ := json.Marshal(ls)
		q.Set("loaders", string(b))
	}
	var vers []mrVersion
	if err := mrGet(ctx, s, "/project/"+url.PathEscape(id)+"/version?"+q.Encode(), &vers); err != nil {
		return fileRef{}, nil, err
	}
	if len(vers) == 0 {
		return fileRef{}, nil, errors.New(a.tr("Нет файла для версии ", "No file for version ") + e.MC)
	}
	// при нескольких загрузчиках (Quilt: quilt+fabric) берём файл под первый подходящий
	v := vers[0]
	if ls := mrLoaders(e.Loader); len(ls) > 1 {
		for _, l := range ls {
			found := false
			for _, x := range vers {
				for _, vl := range x.Loaders {
					if vl == l {
						v, found = x, true
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}
	}
	if len(v.Files) == 0 {
		return fileRef{}, nil, errors.New("no files")
	}
	f := v.Files[0]
	for _, x := range v.Files {
		if x.Primary {
			f = x
		}
	}
	var deps []string
	for _, d := range v.Dependencies {
		if d.Type == "required" && d.ProjectID != "" {
			deps = append(deps, d.ProjectID)
		}
	}
	return fileRef{f.URL, f.Filename, f.Hashes.SHA1}, deps, nil
}

type mrVersion struct {
	ID         string   `json:"id"`
	VersionNum string   `json:"version_number"`
	Loaders    []string `json:"loaders"`
	Files      []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
		Hashes   struct {
			SHA1 string `json:"sha1"`
		} `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID string `json:"project_id"`
		Type      string `json:"dependency_type"`
	} `json:"dependencies"`
}

func (a *App) installProject(ctx context.Context, s Settings, e Eff, src, id, typ, world string, depth int, done map[string]bool) ([]string, error) {
	if done[id] {
		return nil, nil
	}
	done[id] = true
	var f fileRef
	var deps []string
	var err error
	if fid, _ := ctx.Value(fileIDKey{}).(string); fid != "" && depth == 0 {
		f, deps, err = a.pickExact(ctx, s, src, id, fid)
	} else {
		f, deps, err = a.pickFile(ctx, s, e, src, id, typ)
	}
	if err != nil {
		return nil, err
	}
	gdir := a.gameDirE(e)
	rel := typeDirs[typ]
	if typ == "datapack" {
		if world == "" || world != filepath.Base(world) {
			return nil, errors.New(a.tr("Выберите мир для датапака", "Pick a world for the datapack"))
		}
		rel = "saves/" + world + "/datapacks"
	}
	name := filepath.Base(f.name)
	if err := a.fetchFile(ctx, f.url, filepath.Join(gdir, filepath.FromSlash(rel), name), f.sha1, 0, nil); err != nil {
		return nil, err
	}
	files := []string{rel + "/" + name}
	a.idxUpdate(gdir, func(m map[string]idxEntry) {
		k := idxKey(src, typ, id, world)
		old := m[k]
		m[k] = idxEntry{Name: name, Type: typ, World: world, Files: append(files, old.Files...)}
	})
	out := []string{name}
	if depth < 1 && typ == "mod" {
		for _, d := range deps {
			if got, err := a.installProject(ctx, s, e, src, d, "mod", "", depth+1, done); err == nil {
				out = append(out, got...)
			}
		}
	}
	return out, nil
}

// фоновые установки из каталога: не привязаны к запросу и не отменяются при смене вкладки
var catJobs = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func jobKey(src, typ, id, world string) string { return idxKey(src, typ, id, world) }

func (a *App) hCatActive(w http.ResponseWriter, r *http.Request) {
	catJobs.Lock()
	out := map[string]string{}
	for k, v := range catJobs.m {
		out[k] = v
		if v != "busy" {
			delete(catJobs.m, k) // готовое и ошибки отдаются один раз
		}
	}
	catJobs.Unlock()
	jw(w, out)
}

func (a *App) hCatInstall(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID, Type, Src, World, Name, Icon, Build, FileID string }
	if readBody(r, &b) != nil || b.ID == "" {
		jerr(w, 400, "bad request")
		return
	}
	if _, ok := typeDirs[b.Type]; !ok {
		b.Type = "mod"
	}
	s := a.settings()
	if b.Src == "" {
		b.Src = s.ModSource
	}
	if b.Type == "modpack" {
		key := jobKey(b.Src, b.Type, b.ID, "")
		catJobs.Lock()
		if catJobs.m[key] == "busy" {
			catJobs.Unlock()
			jw(w, map[string]any{"queued": true})
			return
		}
		catJobs.m[key] = "busy"
		catJobs.Unlock()
		a.enqueue(key, "pack", func(ctx context.Context) error {
			before := map[string]bool{}
			for _, x := range a.settings().Builds {
				before[x.ID] = true
			}
			err := a.installModpack(ctx, b.Src, b.ID, b.Name, b.Icon)
			if err == nil {
				for _, x := range a.settings().Builds {
					if !before[x.ID] {
						a.packMark(key, x.ID)
					}
				}
			}
			catJobs.Lock()
			if err != nil {
				catJobs.m[key] = "error:" + err.Error()
			} else {
				catJobs.m[key] = "done"
			}
			catJobs.Unlock()
			return err
		})
		jw(w, map[string]any{"queued": true, "pack": true})
		return
	}
	e := s.effFor(b.Build)
	if e.MC == "" {
		jerr(w, 400, a.tr("Сначала выберите версию игры", "Pick a game version first"))
		return
	}
	key := jobKey(b.Src, b.Type, b.ID, b.World)
	catJobs.Lock()
	if catJobs.m[key] == "busy" {
		catJobs.Unlock()
		jw(w, map[string]any{"queued": true})
		return
	}
	catJobs.m[key] = "busy"
	catJobs.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if b.FileID != "" {
			ctx = context.WithValue(ctx, fileIDKey{}, b.FileID)
		}
		files, err := a.installProject(ctx, s, e, b.Src, b.ID, b.Type, b.World, 0, map[string]bool{})
		catJobs.Lock()
		if err != nil {
			catJobs.m[key] = "error:" + err.Error()
			a.lg.add("[Mobilix] " + a.tr("Каталог: ", "Catalog: ") + err.Error())
		} else {
			catJobs.m[key] = "done"
			a.lg.add("[Mobilix] " + a.tr("Установлено: ", "Installed: ") + strings.Join(files, ", "))
		}
		catJobs.Unlock()
	}()
	jw(w, map[string]any{"queued": true})
}

// hCatInstalled: файлы в папке выбранной установки (в т. ч. поставленные вручную).
func (a *App) hCatInstalled(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	if _, ok := typeDirs[typ]; !ok || typeDirs[typ] == "" {
		typ = "mod"
	}
	gdir := a.gameDirE(a.settings().effFor(r.URL.Query().Get("build")))
	out := []map[string]any{}
	add := func(dir, world string) {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".part") || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if e.IsDir() && typ != "shader" && typ != "resourcepack" && typ != "datapack" {
				continue
			}
			info, _ := e.Info()
			out = append(out, map[string]any{"name": e.Name(), "size": info.Size(), "world": world})
		}
	}
	if typ == "datapack" {
		ws, _ := os.ReadDir(filepath.Join(gdir, "saves"))
		for _, w := range ws {
			if w.IsDir() {
				add(filepath.Join(gdir, "saves", w.Name(), "datapacks"), w.Name())
			}
		}
	} else {
		add(filepath.Join(gdir, typeDirs[typ]), "")
	}
	jw(w, map[string]any{"files": out})
}

func (a *App) hCatRemove(w http.ResponseWriter, r *http.Request) {
	var b struct{ Type, Name, World, Build string }
	if readBody(r, &b) != nil || b.Name == "" || b.Name != filepath.Base(b.Name) || b.World != filepath.Base(b.World) && b.World != "" {
		jerr(w, 400, "bad request")
		return
	}
	if _, ok := typeDirs[b.Type]; !ok || typeDirs[b.Type] == "" {
		jerr(w, 400, "bad type")
		return
	}
	gdir := a.gameDirE(a.settings().effFor(b.Build))
	rel := typeDirs[b.Type]
	if b.Type == "datapack" {
		if b.World == "" {
			jerr(w, 400, "world?")
			return
		}
		rel = "saves/" + b.World + "/datapacks"
	}
	_ = os.RemoveAll(filepath.Join(gdir, filepath.FromSlash(rel), b.Name))
	a.idxUpdate(gdir, func(map[string]idxEntry) {})
	jw(w, map[string]any{"ok": true})
}

func (a *App) hWorlds(w http.ResponseWriter, r *http.Request) {
	ents, _ := os.ReadDir(filepath.Join(a.gameDirE(a.settings().effFor(r.URL.Query().Get("build"))), "saves"))
	out := []string{}
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	jw(w, map[string]any{"worlds": out})
}

type fileIDKey struct{}

// pickExact — файл, выбранный пользователем вручную.
func (a *App) pickExact(ctx context.Context, s Settings, src, id, fid string) (fileRef, []string, error) {
	if src == "curseforge" {
		var res struct {
			Data cfFile `json:"data"`
		}
		if err := cfDo(ctx, "GET", "/mods/"+id+"/files/"+url.PathEscape(fid), nil, &res); err != nil {
			return fileRef{}, nil, err
		}
		f := res.Data
		var deps []string
		for _, d := range f.Dependencies {
			if d.Rel == 3 {
				deps = append(deps, strconv.Itoa(d.ModID))
			}
		}
		return fileRef{f.url(), f.FileName, f.sha1()}, deps, nil
	}
	var v mrVersion
	if err := mrGet(ctx, s, "/version/"+url.PathEscape(fid), &v); err != nil {
		return fileRef{}, nil, err
	}
	if len(v.Files) == 0 {
		return fileRef{}, nil, errors.New("no files")
	}
	f := v.Files[0]
	for _, x := range v.Files {
		if x.Primary {
			f = x
		}
	}
	var deps []string
	for _, d := range v.Dependencies {
		if d.Type == "required" && d.ProjectID != "" {
			deps = append(deps, d.ProjectID)
		}
	}
	return fileRef{f.URL, f.Filename, f.Hashes.SHA1}, deps, nil
}

// hCatFiles: список файлов проекта для ручного выбора (как в мобильном лаунчере).
func (a *App) hCatFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ := q.Get("type")
	if _, ok := typeDirs[typ]; !ok {
		typ = "mod"
	}
	s := a.settings()
	src := q.Get("src")
	if src == "" {
		src = s.ModSource
	}
	e := s.effFor(q.Get("build"))
	strict := q.Get("strict") == "1" && e.MC != ""
	id := q.Get("id")
	if id == "" {
		jerr(w, 400, "bad request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	type item struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Ver     string   `json:"ver"`
		MC      []string `json:"mc"`
		Loaders []string `json:"loaders"`
	}
	out := []item{}
	if src == "curseforge" {
		v := url.Values{}
		v.Set("pageSize", "50")
		if strict {
			v.Set("gameVersion", e.MC)
			if m := cfLoader(e.Loader); m > 0 && typ == "mod" {
				v.Set("modLoaderType", strconv.Itoa(m))
			}
		}
		var res struct {
			Data []struct {
				cfFile
				DisplayName string   `json:"displayName"`
				GameVers    []string `json:"gameVersions"`
			} `json:"data"`
		}
		if err := cfDo(ctx, "GET", "/mods/"+id+"/files?"+v.Encode(), nil, &res); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		for _, d := range res.Data {
			it := item{ID: strconv.Itoa(d.ID), Name: d.FileName, Ver: d.DisplayName}
			for _, g := range d.GameVers {
				if len(g) > 0 && g[0] >= '0' && g[0] <= '9' {
					it.MC = append(it.MC, g)
				} else {
					it.Loaders = append(it.Loaders, g)
				}
			}
			out = append(out, it)
		}
	} else {
		qq := url.Values{}
		if strict {
			b, _ := json.Marshal([]string{e.MC})
			qq.Set("game_versions", string(b))
			if ls := mrLoaders(e.Loader); len(ls) > 0 && typ == "mod" {
				b, _ := json.Marshal(ls)
				qq.Set("loaders", string(b))
			}
		}
		var vers []struct {
			mrVersion
			Name     string   `json:"name"`
			GameVers []string `json:"game_versions"`
		}
		if err := mrGet(ctx, s, "/project/"+url.PathEscape(id)+"/version?"+qq.Encode(), &vers); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		for _, d := range vers {
			fn := ""
			if len(d.Files) > 0 {
				fn = d.Files[0].Filename
			}
			out = append(out, item{d.ID, fn, d.VersionNum, d.GameVers, d.Loaders})
		}
	}
	if len(out) > 60 {
		out = out[:60]
	}
	jw(w, map[string]any{"files": out})
}

// сборки, установленные из каталога: ключ проекта -> id сборки (чтобы кнопка не возвращалась в «Установить»)
var packsMu sync.Mutex

func (a *App) packsMap() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(a.path("catalog-packs.json")); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (a *App) packMark(key, buildID string) {
	packsMu.Lock()
	defer packsMu.Unlock()
	m := a.packsMap()
	m[key] = buildID
	b, _ := json.Marshal(m)
	_ = os.WriteFile(a.path("catalog-packs.json"), b, 0o644)
}

func (a *App) packHave(key string) bool {
	packsMu.Lock()
	id := a.packsMap()[key]
	packsMu.Unlock()
	if id == "" {
		return false
	}
	for _, b := range a.settings().Builds {
		if b.ID == id {
			return true
		}
	}
	return false
}

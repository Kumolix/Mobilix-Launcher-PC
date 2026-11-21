package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const modrinth = "https://api.modrinth.com/v2"

var typeDirs = map[string]string{"mod": "mods", "resourcepack": "resourcepacks", "shader": "shaderpacks", "datapack": "datapacks"}

func (a *App) hCatSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ := q.Get("type")
	if _, ok := typeDirs[typ]; !ok {
		typ = "mod"
	}
	s := a.settings()
	facets := [][]string{{"project_type:" + typ}}
	if q.Get("filter") == "1" && s.Selected != "" {
		facets = append(facets, []string{"versions:" + s.Selected})
		if typ == "mod" && s.Loader == "fabric" {
			facets = append(facets, []string{"categories:fabric"})
		}
	}
	fb, _ := json.Marshal(facets)
	off := 0
	fmt.Sscanf(q.Get("offset"), "%d", &off)
	u := fmt.Sprintf("%s/search?query=%s&facets=%s&limit=20&offset=%d", modrinth, url.QueryEscape(q.Get("q")), url.QueryEscape(string(fb)), off)
	var res struct {
		Hits []struct {
			ProjectID   string `json:"project_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			IconURL     string `json:"icon_url"`
			Author      string `json:"author"`
			Downloads   int    `json:"downloads"`
			Slug        string `json:"slug"`
		} `json:"hits"`
		Total int `json:"total_hits"`
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := getJSON(ctx, u, &res); err != nil {
		jerr(w, 502, a.tr("Каталог недоступен: ", "Catalog unavailable: ")+err.Error())
		return
	}
	jw(w, res)
}

type mrVersion struct {
	ID         string `json:"id"`
	VersionNum string `json:"version_number"`
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

func (a *App) catDir(s Settings, typ string) string {
	return filepath.Join(a.gameDir(s), typeDirs[typ])
}

func (a *App) installProject(ctx context.Context, s Settings, id, typ string, depth int, done map[string]bool) ([]string, error) {
	if done[id] {
		return nil, nil
	}
	done[id] = true
	jarr := func(v string) string { b, _ := json.Marshal([]string{v}); return url.QueryEscape(string(b)) }
	u := fmt.Sprintf("%s/project/%s/version?game_versions=%s", modrinth, url.PathEscape(id), jarr(s.Selected))
	if typ == "mod" && s.Loader == "fabric" {
		u += "&loaders=" + jarr("fabric")
	}
	var vers []mrVersion
	if err := getJSON(ctx, u, &vers); err != nil {
		return nil, err
	}
	if len(vers) == 0 {
		return nil, errors.New(a.tr("Нет файла для версии ", "No file for version ") + s.Selected)
	}
	v := vers[0]
	if len(v.Files) == 0 {
		return nil, errors.New("no files")
	}
	f := v.Files[0]
	for _, x := range v.Files {
		if x.Primary {
			f = x
		}
	}
	dir := a.catDir(s, typ)
	name := filepath.Base(f.Filename)
	if err := a.fetchFile(ctx, f.URL, filepath.Join(dir, name), f.Hashes.SHA1, 0, nil); err != nil {
		return nil, err
	}
	out := []string{name}
	if depth < 1 && typ == "mod" {
		for _, d := range v.Dependencies {
			if d.Type == "required" && d.ProjectID != "" {
				if got, err := a.installProject(ctx, s, d.ProjectID, "mod", depth+1, done); err == nil {
					out = append(out, got...)
				}
			}
		}
	}
	return out, nil
}

func (a *App) hCatInstall(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID, Type string }
	if readBody(r, &b) != nil || b.ID == "" {
		jerr(w, 400, "bad request")
		return
	}
	if _, ok := typeDirs[b.Type]; !ok {
		b.Type = "mod"
	}
	s := a.settings()
	if s.Selected == "" {
		jerr(w, 400, a.tr("Сначала выберите версию игры", "Pick a game version first"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	files, err := a.installProject(ctx, s, b.ID, b.Type, 0, map[string]bool{})
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	a.lg.add("[Mobilix] " + a.tr("Установлено: ", "Installed: ") + strings.Join(files, ", "))
	jw(w, map[string]any{"files": files})
}

func (a *App) hCatInstalled(w http.ResponseWriter, r *http.Request) {
	typ := r.URL.Query().Get("type")
	if _, ok := typeDirs[typ]; !ok {
		typ = "mod"
	}
	ents, _ := os.ReadDir(a.catDir(a.settings(), typ))
	out := []map[string]any{}
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".part") {
			continue
		}
		info, _ := e.Info()
		out = append(out, map[string]any{"name": e.Name(), "size": info.Size()})
	}
	jw(w, map[string]any{"files": out})
}

func (a *App) hCatRemove(w http.ResponseWriter, r *http.Request) {
	var b struct{ Type, Name string }
	if readBody(r, &b) != nil || b.Name == "" || b.Name != filepath.Base(b.Name) {
		jerr(w, 400, "bad request")
		return
	}
	if _, ok := typeDirs[b.Type]; !ok {
		jerr(w, 400, "bad type")
		return
	}
	_ = os.Remove(filepath.Join(a.catDir(a.settings(), b.Type), b.Name))
	jw(w, map[string]any{"ok": true})
}

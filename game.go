package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var httpc = &http.Client{Timeout: 0}

const (
	manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	assetsURL   = "https://resources.download.minecraft.net/"
	libsURL     = "https://libraries.minecraft.net/"
)

func httpGet(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mobilix-PC/"+appVersion+" (github.com/Kumolix/Mobilix-Launcher-PC)")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, u)
	}
	return resp, nil
}

func getJSON(ctx context.Context, u string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	resp, err := httpGet(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// ---------- описание версии ----------

type Artifact struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type OSRule struct {
	Name    string `json:"name"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

type Rule struct {
	Action   string          `json:"action"`
	OS       *OSRule         `json:"os"`
	Features map[string]bool `json:"features"`
}

type Library struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Downloads *struct {
		Artifact    *Artifact           `json:"artifact"`
		Classifiers map[string]Artifact `json:"classifiers"`
	} `json:"downloads"`
	Natives map[string]string `json:"natives"`
	Rules   []Rule            `json:"rules"`
	Extract *struct {
		Exclude []string `json:"exclude"`
	} `json:"extract"`
}

type ArgItem struct {
	Value []string
	Rules []Rule
}

func (a *ArgItem) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		a.Value = []string{s}
		return nil
	}
	var o struct {
		Rules []Rule          `json:"rules"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	a.Rules = o.Rules
	if json.Unmarshal(o.Value, &s) == nil {
		a.Value = []string{s}
		return nil
	}
	return json.Unmarshal(o.Value, &a.Value)
}

type Args struct {
	Game []ArgItem `json:"game"`
	JVM  []ArgItem `json:"jvm"`
}

type Version struct {
	ID                 string `json:"id"`
	InheritsFrom       string `json:"inheritsFrom"`
	Type               string `json:"type"`
	MainClass          string `json:"mainClass"`
	MinecraftArguments string `json:"minecraftArguments"`
	Assets             string `json:"assets"`
	Arguments          *Args  `json:"arguments"`
	AssetIndex         *struct {
		ID   string `json:"id"`
		SHA1 string `json:"sha1"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	} `json:"assetIndex"`
	Downloads   map[string]Artifact `json:"downloads"`
	Libraries   []Library           `json:"libraries"`
	JavaVersion *struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Logging *struct {
		Client *struct {
			Argument string   `json:"argument"`
			File     Artifact `json:"file"`
		} `json:"client"`
	} `json:"logging"`

	jarID string
}

func goos() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return runtime.GOOS
}

func ruleMatch(r Rule, feats map[string]bool) bool {
	if r.OS != nil {
		if r.OS.Name != "" && r.OS.Name != goos() {
			return false
		}
		if r.OS.Arch != "" { // x86 / arm64 — у нас 64-битный amd64
			return false
		}
		if r.OS.Version != "" {
			if ok, _ := regexp.MatchString(r.OS.Version, "10.0"); !ok || goos() != "windows" {
				return false
			}
		}
	}
	for k, v := range r.Features {
		if feats[k] != v {
			return false
		}
	}
	return true
}

func rulesOK(rules []Rule, feats map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		if ruleMatch(r, feats) {
			allowed = r.Action == "allow"
		}
	}
	return allowed
}

func (a *App) verPath(id string) string {
	return a.path("shared", "versions", id, id+".json")
}

func (a *App) loadVersion(id string) (*Version, error) {
	b, err := os.ReadFile(a.verPath(id))
	if err != nil {
		return nil, err
	}
	var v Version
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	if v.InheritsFrom == "" {
		v.jarID = v.ID
		return &v, nil
	}
	p, err := a.loadVersion(v.InheritsFrom)
	if err != nil {
		return nil, err
	}
	m := *p
	m.ID = v.ID
	if v.Type != "" {
		m.Type = v.Type
	}
	if v.MainClass != "" {
		m.MainClass = v.MainClass
	}
	if v.MinecraftArguments != "" {
		m.MinecraftArguments = v.MinecraftArguments
	}
	m.Libraries = append(append([]Library{}, v.Libraries...), p.Libraries...)
	if v.Arguments != nil {
		na := &Args{}
		if p.Arguments != nil {
			na.Game = append(na.Game, p.Arguments.Game...)
			na.JVM = append(na.JVM, p.Arguments.JVM...)
		}
		na.Game = append(na.Game, v.Arguments.Game...)
		na.JVM = append(na.JVM, v.Arguments.JVM...)
		m.Arguments = na
	}
	if v.JavaVersion != nil {
		m.JavaVersion = v.JavaVersion
	}
	m.jarID = p.jarID
	return &m, nil
}

func (a *App) installedVersions() []string {
	ents, _ := os.ReadDir(a.path("shared", "versions"))
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			if _, err := os.Stat(a.verPath(e.Name())); err == nil {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---------- задания и статус ----------

func (a *App) status() map[string]any {
	a.jmu.Lock()
	defer a.jmu.Unlock()
	pct := -1
	if a.total > 0 {
		pct = a.done * 100 / a.total
		if pct > 100 {
			pct = 100
		}
	}
	a.gmu.Lock()
	running := a.gameCmd != nil
	a.gmu.Unlock()
	return map[string]any{"busy": a.busy, "kind": a.kind, "stage": a.stage, "percent": pct, "error": a.errMsg, "running": running}
}

func (a *App) setStage(s string, done, total int) {
	a.jmu.Lock()
	a.stage, a.done, a.total = s, done, total
	a.jmu.Unlock()
}

func (a *App) startJob(kind string) error {
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
		err := a.runJob(ctx, kind)
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

func (a *App) runJob(ctx context.Context, kind string) error {
	s := a.settings()
	if s.Selected == "" {
		return errors.New(a.tr("Выберите версию Minecraft", "Pick a Minecraft version"))
	}
	if kind == "play" {
		a.rotateLog()
	}
	prof, err := a.ensureProfile(ctx, s)
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
	return a.launch(v, s, java)
}

// ---------- манифест, Fabric ----------

type ManifestEntry struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	ReleaseTime string `json:"releaseTime"`
	SHA1        string `json:"sha1"`
}

type Manifest struct {
	Latest   map[string]string `json:"latest"`
	Versions []ManifestEntry   `json:"versions"`
}

func (a *App) manifest(ctx context.Context) (*Manifest, error) {
	cache := a.path("cache", "version_manifest.json")
	var m Manifest
	err := getJSON(ctx, manifestURL, &m)
	if err == nil && len(m.Versions) > 0 {
		_ = os.MkdirAll(filepath.Dir(cache), 0o755)
		b, _ := json.Marshal(m)
		_ = os.WriteFile(cache, b, 0o644)
		return &m, nil
	}
	if b, e := os.ReadFile(cache); e == nil && json.Unmarshal(b, &m) == nil && len(m.Versions) > 0 {
		return &m, nil
	}
	if err == nil {
		err = errors.New("empty manifest")
	}
	return nil, err
}

func (a *App) hVersions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	m, err := a.manifest(ctx)
	if err != nil {
		jerr(w, 502, a.tr("Не удалось получить список версий (нет интернета?): ", "Could not load the version list (offline?): ")+err.Error())
		return
	}
	type V struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Date string `json:"date"`
	}
	out := make([]V, 0, len(m.Versions))
	for _, e := range m.Versions {
		d := e.ReleaseTime
		if len(d) >= 10 {
			d = d[:10]
		}
		out = append(out, V{e.ID, e.Type, d})
	}
	jw(w, map[string]any{"latest": m.Latest, "versions": out})
}

func (a *App) fabricLoaders(ctx context.Context, mc string) ([]string, error) {
	var list []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	if err := getJSON(ctx, "https://meta.fabricmc.net/v2/versions/loader/"+mc, &list); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range list {
		if e.Loader.Stable {
			out = append(out, e.Loader.Version)
		}
	}
	if len(out) == 0 {
		for _, e := range list {
			out = append(out, e.Loader.Version)
		}
	}
	if len(out) == 0 {
		return nil, errors.New(a.tr("Для этой версии нет Fabric", "No Fabric for this version"))
	}
	return out, nil
}

func (a *App) hFabric(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	l, err := a.fabricLoaders(ctx, r.URL.Query().Get("mc"))
	if err != nil {
		jerr(w, 502, err.Error())
		return
	}
	if len(l) > 12 {
		l = l[:12]
	}
	jw(w, map[string]any{"loaders": l})
}

// ensureProfile скачивает json нужной версии (и Fabric), возвращает id профиля.
func (a *App) ensureProfile(ctx context.Context, s Settings) (string, error) {
	a.setStage(a.tr("Список версий", "Version list"), 0, 0)
	mc := s.Selected
	if _, err := os.Stat(a.verPath(mc)); err != nil {
		m, err := a.manifest(ctx)
		if err != nil {
			return "", fmt.Errorf("%s: %w", a.tr("нет доступа к серверу версий", "cannot reach the version server"), err)
		}
		var ent *ManifestEntry
		for i := range m.Versions {
			if m.Versions[i].ID == mc {
				ent = &m.Versions[i]
			}
		}
		if ent == nil {
			return "", errors.New(a.tr("Версия не найдена: ", "Version not found: ") + mc)
		}
		if err := a.fetchFile(ctx, ent.URL, a.verPath(mc), ent.SHA1, 0, nil); err != nil {
			return "", err
		}
	}
	if s.Loader != "fabric" {
		return mc, nil
	}
	lv := s.LoaderVer
	if lv == "" {
		l, err := a.fabricLoaders(ctx, mc)
		if err != nil {
			// без сети можно использовать уже установленный Fabric
			for _, id := range a.installedVersions() {
				if strings.HasPrefix(id, "fabric-loader-") && strings.HasSuffix(id, "-"+mc) {
					return id, nil
				}
			}
			return "", err
		}
		lv = l[0]
	}
	id := "fabric-loader-" + lv + "-" + mc
	if _, err := os.Stat(a.verPath(id)); err != nil {
		u := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json", mc, lv)
		if err := a.fetchFile(ctx, u, a.verPath(id), "", 0, nil); err != nil {
			return "", err
		}
	}
	return id, nil
}

// ---------- загрузка файлов ----------

type dlItem struct {
	url, dest, sha1 string
	size            int64
}

func fileOK(p string, size int64, sum string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	if size > 0 {
		return st.Size() == size
	}
	if sum != "" {
		return fileSHA1(p) == sum
	}
	return st.Size() > 0
}

func fileSHA1(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha1.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// fetchFile качает файл (до 3 попыток), проверяет sha1; onBytes получает число скачанных байт.
func (a *App) fetchFile(ctx context.Context, u, dest, sum string, size int64, onBytes func(int64)) error {
	if fileOK(dest, size, sum) {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	var last error
	for try := 0; try < 3; try++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last = func() error {
			resp, err := httpGet(ctx, u)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			tmp := dest + ".part"
			f, err := os.Create(tmp)
			if err != nil {
				return err
			}
			h := sha1.New()
			var rd io.Reader = resp.Body
			if onBytes != nil {
				rd = &countReader{r: resp.Body, f: onBytes}
			}
			_, err = io.Copy(io.MultiWriter(f, h), rd)
			f.Close()
			if err != nil {
				os.Remove(tmp)
				return err
			}
			if sum != "" && hex.EncodeToString(h.Sum(nil)) != sum {
				os.Remove(tmp)
				return errors.New("sha1 mismatch: " + u)
			}
			return os.Rename(tmp, dest)
		}()
		if last == nil {
			return nil
		}
		time.Sleep(time.Duration(try+1) * 700 * time.Millisecond)
	}
	return last
}

type countReader struct {
	r io.Reader
	f func(int64)
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.f(int64(n))
	}
	return n, err
}

func (a *App) fetchAll(ctx context.Context, label string, items []dlItem, threads int) error {
	if len(items) == 0 {
		return nil
	}
	if threads < 1 {
		threads = 8
	}
	var done int64
	total := len(items)
	a.setStage(label, 0, total)
	ch := make(chan dlItem)
	var wg sync.WaitGroup
	var firstErr atomic.Value
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range ch {
				if cctx.Err() != nil {
					continue
				}
				if err := a.fetchFile(cctx, it.url, it.dest, it.sha1, it.size, nil); err != nil {
					firstErr.CompareAndSwap(nil, err)
					cancel()
					continue
				}
				n := atomic.AddInt64(&done, 1)
				a.setStage(label, int(n), total)
			}
		}()
	}
	for _, it := range items {
		ch <- it
	}
	close(ch)
	wg.Wait()
	if e := firstErr.Load(); e != nil {
		return e.(error)
	}
	return ctx.Err()
}

// ---------- библиотеки, ресурсы ----------

func mavenPath(name string) (string, bool) {
	p := strings.Split(strings.SplitN(name, "@", 2)[0], ":")
	if len(p) < 3 {
		return "", false
	}
	file := p[1] + "-" + p[2]
	if len(p) >= 4 {
		file += "-" + p[3]
	}
	return strings.ReplaceAll(p[0], ".", "/") + "/" + p[1] + "/" + p[2] + "/" + file + ".jar", true
}

func libKey(name string) string {
	p := strings.Split(name, ":")
	if len(p) >= 4 {
		return p[0] + ":" + p[1] + ":" + p[3]
	}
	if len(p) >= 2 {
		return p[0] + ":" + p[1]
	}
	return name
}

type libFile struct {
	art     Artifact
	native  bool
	exclude []string
}

func (a *App) collectLibs(v *Version) (cp []libFile, natives []libFile) {
	feats := map[string]bool{}
	seen := map[string]bool{}
	for _, l := range v.Libraries {
		if !rulesOK(l.Rules, feats) {
			continue
		}
		if l.Natives != nil {
			cls, ok := l.Natives[goos()]
			if !ok {
				continue
			}
			cls = strings.ReplaceAll(cls, "${arch}", "64")
			if l.Downloads != nil {
				if art, ok := l.Downloads.Classifiers[cls]; ok {
					lf := libFile{art: art, native: true}
					if l.Extract != nil {
						lf.exclude = l.Extract.Exclude
					}
					natives = append(natives, lf)
				}
			}
			continue
		}
		k := libKey(l.Name)
		if seen[k] {
			continue
		}
		seen[k] = true
		var art Artifact
		if l.Downloads != nil && l.Downloads.Artifact != nil {
			art = *l.Downloads.Artifact
		} else if p, ok := mavenPath(l.Name); ok {
			base := l.URL
			if base == "" {
				base = libsURL
			}
			if !strings.HasSuffix(base, "/") {
				base += "/"
			}
			art = Artifact{Path: p, URL: base + p}
		} else {
			continue
		}
		cp = append(cp, libFile{art: art})
	}
	return
}

func (a *App) ensureFiles(ctx context.Context, v *Version, s Settings) error {
	libs, nat := a.collectLibs(v)
	var items []dlItem
	for _, l := range append(append([]libFile{}, libs...), nat...) {
		items = append(items, dlItem{l.art.URL, a.path("shared", "libraries", filepath.FromSlash(l.art.Path)), l.art.SHA1, l.art.Size})
	}
	if c, ok := v.Downloads["client"]; ok {
		items = append(items, dlItem{c.URL, a.path("shared", "versions", v.jarID, v.jarID+".jar"), c.SHA1, c.Size})
	}
	if v.Logging != nil && v.Logging.Client != nil && v.Logging.Client.File.URL != "" {
		f := v.Logging.Client.File
		items = append(items, dlItem{f.URL, a.path("shared", "assets", "log_configs", filepath.Base(f.URL[strings.LastIndex(f.URL, "/")+1:])), f.SHA1, f.Size})
	}
	if err := a.fetchAll(ctx, a.tr("Библиотеки", "Libraries"), items, s.Threads); err != nil {
		return err
	}
	if err := a.extractNatives(v, nat); err != nil {
		return err
	}
	return a.ensureAssets(ctx, v, s)
}

func (a *App) nativesDir(v *Version) string {
	return a.path("shared", "versions", v.ID, "natives")
}

func (a *App) extractNatives(v *Version, nat []libFile) error {
	dir := a.nativesDir(v)
	if len(nat) == 0 {
		return os.MkdirAll(dir, 0o755)
	}
	marker := filepath.Join(dir, ".done")
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	_ = os.MkdirAll(dir, 0o755)
	for _, n := range nat {
		jar := a.path("shared", "libraries", filepath.FromSlash(n.art.Path))
		if err := unzip(jar, dir, func(name string) bool {
			if strings.HasPrefix(name, "META-INF/") {
				return false
			}
			for _, e := range n.exclude {
				if strings.HasPrefix(name, e) {
					return false
				}
			}
			return true
		}); err != nil {
			return err
		}
	}
	return os.WriteFile(marker, []byte("ok"), 0o644)
}

func unzip(zipPath, dest string, keep func(string) bool) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	root := filepath.Clean(dest) + string(os.PathSeparator)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || (keep != nil && !keep(f.Name)) {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(target, root) {
			continue // защита от zip-slip
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type assetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
	Virtual  bool `json:"virtual"`
	MapToRes bool `json:"map_to_resources"`
}

func (a *App) assetsRoot() string { return a.path("shared", "assets") }

func (a *App) ensureAssets(ctx context.Context, v *Version, s Settings) error {
	if v.AssetIndex == nil {
		return nil
	}
	idxPath := filepath.Join(a.assetsRoot(), "indexes", v.AssetIndex.ID+".json")
	if err := a.fetchFile(ctx, v.AssetIndex.URL, idxPath, v.AssetIndex.SHA1, 0, nil); err != nil {
		return err
	}
	b, err := os.ReadFile(idxPath)
	if err != nil {
		return err
	}
	var idx assetIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		return err
	}
	var items []dlItem
	for _, o := range idx.Objects {
		p := o.Hash[:2] + "/" + o.Hash
		items = append(items, dlItem{assetsURL + p, filepath.Join(a.assetsRoot(), "objects", o.Hash[:2], o.Hash), o.Hash, o.Size})
	}
	if err := a.fetchAll(ctx, a.tr("Ресурсы игры", "Game assets"), items, s.Threads*2); err != nil {
		return err
	}
	// старые версии: ресурсы лежат по обычным именам
	if idx.Virtual || idx.MapToRes {
		root := filepath.Join(a.assetsRoot(), "virtual", "legacy")
		if idx.MapToRes {
			root = filepath.Join(a.gameDir(s), "resources")
		}
		for name, o := range idx.Objects {
			dst := filepath.Join(root, filepath.FromSlash(name))
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(dst), 0o755)
			src := filepath.Join(a.assetsRoot(), "objects", o.Hash[:2], o.Hash)
			if in, err := os.ReadFile(src); err == nil {
				_ = os.WriteFile(dst, in, 0o644)
			}
		}
	}
	return nil
}

// ---------- Java ----------

var javaVerRe = regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)

func javaMajor(exe string) int {
	c := exec.Command(exe, "-version")
	hideWindow(c)
	out, err := c.CombinedOutput()
	if err != nil {
		return 0
	}
	m := javaVerRe.FindStringSubmatch(string(out))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	if n == 1 && m[2] != "" {
		n, _ = strconv.Atoi(m[2]) // 1.8 -> 8
	}
	return n
}

func javaOK(req, have int) bool {
	if have == 0 {
		return false
	}
	if req <= 8 {
		return have == 8
	}
	return have >= req
}

func javaName() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

func (a *App) findJava(req int, s Settings) string {
	if s.JavaPath != "" {
		if st, err := os.Stat(s.JavaPath); err == nil {
			p := s.JavaPath
			if st.IsDir() {
				p = filepath.Join(p, "bin", javaName())
			}
			if m := javaMajor(p); m > 0 {
				if !javaOK(req, m) {
					a.lg.add(fmt.Sprintf("[Mobilix] "+a.tr("Внимание: нужна Java %d, указана Java %d", "Warning: Java %d required, %d configured"), req, m))
				}
				return p
			}
		}
	}
	var c []string
	if m, _ := filepath.Glob(a.path("runtimes", itoa(req), "*", "bin", javaName())); len(m) > 0 {
		c = append(c, m...)
	}
	if h := os.Getenv("JAVA_HOME"); h != "" {
		c = append(c, filepath.Join(h, "bin", javaName()))
	}
	if p, err := exec.LookPath("java"); err == nil {
		c = append(c, p)
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		for _, pat := range []string{"Java/*/bin/java.exe", "Eclipse Adoptium/*/bin/java.exe", "Microsoft/jdk-*/bin/java.exe", "Zulu/*/bin/java.exe", "BellSoft/*/bin/java.exe"} {
			m, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pat)))
			c = append(c, m...)
		}
	}
	for _, p := range c {
		if javaOK(req, javaMajor(p)) {
			return p
		}
	}
	return ""
}

func (a *App) ensureJava(ctx context.Context, req int, s Settings) (string, error) {
	if req == 16 {
		req = 17
	}
	if p := a.findJava(req, s); p != "" {
		return p, nil
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return "", fmt.Errorf("Java %d not found", req)
	}
	a.lg.add(fmt.Sprintf("[Mobilix] "+a.tr("Скачиваю Java %d (Eclipse Temurin)…", "Downloading Java %d (Eclipse Temurin)…"), req))
	osn, ext := "windows", "zip"
	if runtime.GOOS == "linux" {
		osn, ext = "linux", "tar.gz"
	}
	u := fmt.Sprintf("https://api.adoptium.net/v3/binary/latest/%d/ga/%s/x64/jre/hotspot/normal/eclipse", req, osn)
	resp, err := httpGet(ctx, u)
	if err != nil {
		return "", fmt.Errorf("%s: %w", a.tr("не удалось скачать Java", "could not download Java"), err)
	}
	defer resp.Body.Close()
	tmp := a.path("cache", fmt.Sprintf("jre-%d.%s", req, ext))
	_ = os.MkdirAll(filepath.Dir(tmp), 0o755)
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	total := int(resp.ContentLength / 1024)
	var got int64
	_, err = io.Copy(f, &countReader{r: resp.Body, f: func(n int64) {
		got += n
		a.setStage(fmt.Sprintf("Java %d", req), int(got/1024), total)
	}})
	f.Close()
	if err != nil {
		return "", err
	}
	a.setStage(fmt.Sprintf(a.tr("Распаковка Java %d", "Unpacking Java %d"), req), 0, 0)
	dst := a.path("runtimes", itoa(req))
	_ = os.RemoveAll(dst)
	if ext == "zip" {
		err = unzip(tmp, dst, nil)
	} else {
		err = untargz(tmp, dst)
	}
	if err != nil {
		return "", err
	}
	_ = os.Remove(tmp)
	if p := a.findJava(req, Settings{}); p != "" {
		return p, nil
	}
	return "", errors.New("Java unpack failed")
}

// ---------- запуск ----------

func (a *App) gameDir(s Settings) string {
	if !s.Isolate {
		return a.path("game")
	}
	name := s.Selected
	if s.Loader == "fabric" {
		name = "fabric-" + s.Selected
	}
	return a.path("instances", name)
}

func offlineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name))
	h[6] = (h[6] & 0x0f) | 0x30
	h[8] = (h[8] & 0x3f) | 0x80
	x := hex.EncodeToString(h[:])
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}

func (a *App) launch(v *Version, s Settings, java string) error {
	a.setStage(a.tr("Запуск игры", "Starting the game"), 0, 0)
	libs, _ := a.collectLibs(v)
	var cp []string
	for _, l := range libs {
		cp = append(cp, a.path("shared", "libraries", filepath.FromSlash(l.art.Path)))
	}
	cp = append(cp, a.path("shared", "versions", v.jarID, v.jarID+".jar"))
	gdir := a.gameDir(s)
	_ = os.MkdirAll(gdir, 0o755)
	assetsID := v.Assets
	if v.AssetIndex != nil {
		assetsID = v.AssetIndex.ID
	}
	gameAssets := a.assetsRoot()
	if assetsID == "legacy" || assetsID == "pre-1.6" {
		if assetsID == "legacy" {
			gameAssets = filepath.Join(a.assetsRoot(), "virtual", "legacy")
		} else {
			gameAssets = filepath.Join(gdir, "resources")
		}
	}
	vtype := v.Type
	if vtype == "" {
		vtype = "release"
	}
	repl := map[string]string{
		"auth_player_name": s.Nick, "version_name": v.ID, "game_directory": gdir, "assets_root": a.assetsRoot(),
		"assets_index_name": assetsID, "auth_uuid": offlineUUID(s.Nick), "auth_access_token": "0", "clientid": "", "auth_xuid": "",
		"user_type": "legacy", "version_type": vtype, "user_properties": "{}", "auth_session": "0", "game_assets": gameAssets,
		"natives_directory": a.nativesDir(v), "launcher_name": "Mobilix", "launcher_version": appVersion,
		"classpath": strings.Join(cp, string(os.PathListSeparator)), "library_directory": a.path("shared", "libraries"),
		"classpath_separator": string(os.PathListSeparator), "primary_jar": a.path("shared", "versions", v.jarID, v.jarID+".jar"),
		"resolution_width": "854", "resolution_height": "480",
	}
	sub := func(x string) string {
		for k, val := range repl {
			x = strings.ReplaceAll(x, "${"+k+"}", val)
		}
		return x
	}
	feats := map[string]bool{}
	expand := func(items []ArgItem) []string {
		var out []string
		for _, it := range items {
			if !rulesOK(it.Rules, feats) {
				continue
			}
			for _, x := range it.Value {
				out = append(out, sub(x))
			}
		}
		return out
	}
	args := []string{fmt.Sprintf("-Xmx%dM", s.RamMB), "-Xms256M", "-Dlog4j2.formatMsgNoLookups=true"}
	if m := javaMajor(java); m >= 9 || m == 0 {
		args = append(args, "-XX:+UseG1GC")
	}
	args = append(args, strings.Fields(s.JvmArgs)...)
	if v.Logging != nil && v.Logging.Client != nil && v.Logging.Client.Argument != "" {
		lf := a.path("shared", "assets", "log_configs", filepath.Base(v.Logging.Client.File.URL[strings.LastIndex(v.Logging.Client.File.URL, "/")+1:]))
		args = append(args, strings.ReplaceAll(v.Logging.Client.Argument, "${path}", lf))
	}
	var jvm, game []string
	if v.Arguments != nil {
		jvm = expand(v.Arguments.JVM)
		game = expand(v.Arguments.Game)
	} else {
		jvm = []string{sub("-Djava.library.path=${natives_directory}"), "-cp", sub("${classpath}")}
		for _, x := range strings.Fields(v.MinecraftArguments) {
			game = append(game, sub(x))
		}
	}
	hasCP := false
	for _, x := range jvm {
		if x == "-cp" || x == "-classpath" {
			hasCP = true
		}
	}
	if !hasCP {
		jvm = append(jvm, "-cp", repl["classpath"])
	}
	if s.Fullscreen {
		game = append(game, "--fullscreen")
	}
	full := append(append(append(args, jvm...), v.MainClass), game...)
	a.lg.add("[Mobilix] " + a.tr("Java: ", "Java: ") + java)
	a.lg.add("[Mobilix] " + a.tr("Папка игры: ", "Game folder: ") + gdir)
	cmd := exec.Command(java, full...)
	cmd.Dir = gdir
	hideWindow(cmd)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	a.gmu.Lock()
	a.gameCmd = cmd
	a.gmu.Unlock()
	a.lg.add("[Mobilix] " + a.tr("Игра запущена", "Game started"))
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			a.lg.add(sc.Text())
		}
		err := cmd.Wait()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		a.gmu.Lock()
		a.gameCmd = nil
		a.gmu.Unlock()
		a.lg.add(fmt.Sprintf("[Mobilix] "+a.tr("Игра закрыта (код %d)", "Game exited (code %d)"), code))
	}()
	if s.CloseOnPlay {
		go func() { time.Sleep(4 * time.Second); a.shutdown() }()
	}
	return nil
}

// ---------- скин ----------

func (a *App) hSkin(w http.ResponseWriter, r *http.Request) {
	s := a.settings()
	src := r.URL.Query().Get("src")
	if src == "" {
		src = s.SkinSrc
	}
	nick := r.URL.Query().Get("nick")
	if nick == "" {
		nick = s.Nick
	}
	nick = cleanNick(nick)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cache := a.path("skins", src+"_"+strings.ToLower(nick)+".png")
	var png []byte
	model := "default"
	var err error
	switch src {
	case "file":
		png, err = os.ReadFile(a.path("skins", "custom.png"))
	case "ely":
		png, model, err = fetchElySkin(ctx, nick)
	default:
		png, model, err = fetchMojangSkin(ctx, nick)
	}
	if err == nil && src != "file" {
		_ = os.MkdirAll(a.path("skins"), 0o755)
		_ = os.WriteFile(cache, png, 0o644)
		_ = os.WriteFile(cache+".model", []byte(model), 0o644)
	}
	if err != nil && src != "file" {
		if b, e := os.ReadFile(cache); e == nil {
			png, err = b, nil
			if m, e := os.ReadFile(cache + ".model"); e == nil {
				model = string(m)
			}
		}
	}
	if err != nil {
		jerr(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Skin-Model", model)
	_, _ = w.Write(png)
}

func httpsURL(u string) string { return strings.Replace(u, "http://", "https://", 1) }

func getBytes(ctx context.Context, u string) ([]byte, error) {
	resp, err := httpGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

type texInfo struct {
	SKIN struct {
		URL      string `json:"url"`
		Metadata struct {
			Model string `json:"model"`
		} `json:"metadata"`
	} `json:"SKIN"`
}

func fetchMojangSkin(ctx context.Context, nick string) ([]byte, string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := getJSON(ctx, "https://api.mojang.com/users/profiles/minecraft/"+nick, &p); err != nil || p.ID == "" {
		return nil, "", errors.New("skin: player not found")
	}
	var prof struct {
		Properties []struct{ Name, Value string } `json:"properties"`
	}
	if err := getJSON(ctx, "https://sessionserver.mojang.com/session/minecraft/profile/"+p.ID, &prof); err != nil {
		return nil, "", err
	}
	for _, pr := range prof.Properties {
		if pr.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(pr.Value)
		if err != nil {
			continue
		}
		var t struct {
			Textures texInfo `json:"textures"`
		}
		if json.Unmarshal(raw, &t) != nil || t.Textures.SKIN.URL == "" {
			continue
		}
		b, err := getBytes(ctx, httpsURL(t.Textures.SKIN.URL))
		m := "default"
		if t.Textures.SKIN.Metadata.Model == "slim" {
			m = "slim"
		}
		return b, m, err
	}
	return nil, "", errors.New("skin: none")
}

func fetchElySkin(ctx context.Context, nick string) ([]byte, string, error) {
	var t texInfo
	if err := getJSON(ctx, "https://skinsystem.ely.by/textures/"+nick, &t); err != nil || t.SKIN.URL == "" {
		return nil, "", errors.New("skin: player not found")
	}
	b, err := getBytes(ctx, httpsURL(t.SKIN.URL))
	m := "default"
	if t.SKIN.Metadata.Model == "slim" {
		m = "slim"
	}
	return b, m, err
}

func untargz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	root := filepath.Clean(dest) + string(os.PathSeparator)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(target, root) {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if !strings.Contains(h.Linkname, "..") {
				_ = os.MkdirAll(filepath.Dir(target), 0o755)
				_ = os.Symlink(h.Linkname, target)
			}
		}
	}
}

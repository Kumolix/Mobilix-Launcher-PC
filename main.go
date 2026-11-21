package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed ui
var uiFS embed.FS

const appVersion = "1.0.0"
const fixedPort = 52871

func itoa(n int) string { return strconv.Itoa(n) }

// ---------- настройки ----------

type Settings struct {
	Lang        string `json:"lang"`     // ru | en
	Style       string `json:"style"`    // new (графит) | old (классика, зелёный)
	Light       bool   `json:"light"`    // светлая тема
	MenuType    string `json:"menuType"` // side (по наведению) | bottom
	Aurora      bool   `json:"aurora"`
	Nick        string `json:"nick"`
	SkinSrc     string `json:"skinSrc"`   // mojang | ely | file
	SkinModel   int    `json:"skinModel"` // 0 авто, 1 широкий, 2 стройный
	RamMB       int    `json:"ramMb"`
	JavaPath    string `json:"javaPath"`
	JvmArgs     string `json:"jvmArgs"`
	Fullscreen  bool   `json:"fullscreen"`
	Threads     int    `json:"threads"`
	Isolate     bool   `json:"isolate"`
	CloseOnPlay bool   `json:"closeOnPlay"`
	Selected    string `json:"selected"`
	Loader      string `json:"loader"` // vanilla | fabric
	LoaderVer   string `json:"loaderVer"`
}

func defaultSettings() Settings {
	return Settings{Lang: "ru", Style: "new", MenuType: "side", Aurora: true, Nick: "Player", SkinSrc: "mojang",
		RamMB: 4096, Threads: 8, Isolate: true, Loader: "vanilla"}
}

type App struct {
	dataDir string
	token   string
	port    int

	mu  sync.Mutex
	set Settings

	jmu    sync.Mutex
	busy   bool
	kind   string
	stage  string
	done   int
	total  int
	errMsg string
	cancel context.CancelFunc

	gmu     sync.Mutex
	gameCmd *exec.Cmd

	lg *Logger

	lastPing  time.Time
	pinged    bool
	browser   *exec.Cmd
	updateTag string
}

func (a *App) settings() Settings { a.mu.Lock(); defer a.mu.Unlock(); return a.set }

func (a *App) saveSettings(mut func(*Settings)) {
	a.mu.Lock()
	mut(&a.set)
	b, _ := json.MarshalIndent(a.set, "", "  ")
	a.mu.Unlock()
	_ = os.WriteFile(filepath.Join(a.dataDir, "settings.json"), b, 0o644)
}

func (a *App) loadSettings() {
	a.set = defaultSettings()
	if b, err := os.ReadFile(filepath.Join(a.dataDir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.set)
	}
	if a.set.Lang == "" {
		a.set.Lang = "ru"
	}
	if a.set.Threads < 1 {
		a.set.Threads = 8
	}
	if a.set.RamMB < 512 {
		a.set.RamMB = 2048
	}
}

func (a *App) tr(ru, en string) string {
	if a.settings().Lang == "en" {
		return en
	}
	return ru
}

func (a *App) path(parts ...string) string {
	return filepath.Join(append([]string{a.dataDir}, parts...)...)
}

// ---------- HTTP ----------

func jw(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func jerr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}

func (a *App) api(mux *http.ServeMux, p string, h http.HandlerFunc) {
	mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.Header.Get("X-K") != a.token {
			http.Error(w, "forbidden", 403)
			return
		}
		h(w, r)
	})
}

func readBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(uiFS, "ui")
	fsrv := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fsrv.ServeHTTP(w, r)
	})

	a.api(mux, "/api/ping", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.lastPing = time.Now()
		a.pinged = true
		a.mu.Unlock()
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/state", func(w http.ResponseWriter, r *http.Request) {
		jw(w, map[string]any{
			"version": appVersion, "settings": a.settings(), "installed": a.installedVersions(),
			"ramMb": totalRAMMB(), "dataDir": a.dataDir, "update": a.updateTag,
			"status": a.status(),
		})
	})
	a.api(mux, "/api/settings", func(w http.ResponseWriter, r *http.Request) {
		var patch map[string]any
		if readBody(r, &patch) != nil {
			jerr(w, 400, "bad json")
			return
		}
		a.saveSettings(func(s *Settings) {
			b, _ := json.Marshal(s)
			var cur map[string]any
			_ = json.Unmarshal(b, &cur)
			for k, v := range patch {
				cur[k] = v
			}
			b, _ = json.Marshal(cur)
			_ = json.Unmarshal(b, s)
			s.Nick = cleanNick(s.Nick)
		})
		jw(w, map[string]any{"settings": a.settings()})
	})
	a.api(mux, "/api/status", func(w http.ResponseWriter, r *http.Request) { jw(w, a.status()) })
	a.api(mux, "/api/versions", a.hVersions)
	a.api(mux, "/api/fabric", a.hFabric)
	a.api(mux, "/api/play", func(w http.ResponseWriter, r *http.Request) {
		if err := a.startJob("play"); err != nil {
			jerr(w, 409, err.Error())
			return
		}
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/install", func(w http.ResponseWriter, r *http.Request) {
		if err := a.startJob("install"); err != nil {
			jerr(w, 409, err.Error())
			return
		}
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/cancel", func(w http.ResponseWriter, r *http.Request) {
		a.jmu.Lock()
		if a.cancel != nil {
			a.cancel()
		}
		a.jmu.Unlock()
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/stop", func(w http.ResponseWriter, r *http.Request) {
		a.gmu.Lock()
		if a.gameCmd != nil && a.gameCmd.Process != nil {
			killTree(a.gameCmd.Process.Pid)
		}
		a.gmu.Unlock()
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/log", func(w http.ResponseWriter, r *http.Request) {
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		lines, next := a.lg.since(from)
		jw(w, map[string]any{"lines": lines, "next": next})
	})
	a.api(mux, "/api/log/file", func(w http.ResponseWriter, r *http.Request) {
		name := "latest.log"
		if r.URL.Query().Get("prev") == "1" {
			name = "previous.log"
		}
		b, _ := os.ReadFile(a.path("logs", name))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(b)
	})
	a.api(mux, "/api/skin", a.hSkin)
	a.api(mux, "/api/skin/import", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if len(b) < 8 || string(b[1:4]) != "PNG" {
			jerr(w, 400, a.tr("Нужен PNG-файл скина", "A PNG skin file is required"))
			return
		}
		_ = os.MkdirAll(a.path("skins"), 0o755)
		_ = os.WriteFile(a.path("skins", "custom.png"), b, 0o644)
		a.saveSettings(func(s *Settings) { s.SkinSrc = "file" })
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/catalog/search", a.hCatSearch)
	a.api(mux, "/api/catalog/install", a.hCatInstall)
	a.api(mux, "/api/catalog/installed", a.hCatInstalled)
	a.api(mux, "/api/catalog/remove", a.hCatRemove)
	a.api(mux, "/api/wipe", func(w http.ResponseWriter, r *http.Request) {
		a.jmu.Lock()
		b := a.busy
		a.jmu.Unlock()
		a.gmu.Lock()
		running := a.gameCmd != nil
		a.gmu.Unlock()
		if b || running {
			jerr(w, 409, a.tr("Сначала дождитесь конца загрузки или закройте игру", "Wait for the download to finish or close the game first"))
			return
		}
		for _, d := range []string{"shared/versions", "shared/libraries", "shared/assets", "runtimes", "cache"} {
			_ = os.RemoveAll(a.path(filepath.FromSlash(d)))
		}
		a.lg.add("[Mobilix] " + a.tr("Скачанные файлы удалены (миры и моды сохранены)", "Downloads deleted (worlds and mods kept)"))
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/openfolder", func(w http.ResponseWriter, r *http.Request) {
		which := r.URL.Query().Get("which")
		dir := a.path()
		if which == "game" {
			dir = a.gameDir(a.settings())
		}
		_ = os.MkdirAll(dir, 0o755)
		openPath(dir)
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/openurl", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			URL string `json:"url"`
		}
		_ = readBody(r, &b)
		u, err := url.Parse(b.URL)
		ok := map[string]bool{"t.me": true, "github.com": true, "modrinth.com": true, "www.tiktok.com": true}
		if err != nil || u.Scheme != "https" || !ok[u.Host] {
			jerr(w, 400, "url not allowed")
			return
		}
		openURL(b.URL)
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/exit", func(w http.ResponseWriter, r *http.Request) {
		jw(w, map[string]any{"ok": true})
		go func() { time.Sleep(250 * time.Millisecond); a.shutdown() }()
	})
	return mux
}

func (a *App) shutdown() {
	if a.browser != nil && a.browser.Process != nil {
		killTree(a.browser.Process.Pid)
	}
	os.Exit(0)
}

// ---------- окно ----------

func findBrowser() string {
	if p := os.Getenv("MOBILIX_BROWSER"); p != "" {
		return p
	}
	pf, pf86, la := os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")
	var c []string
	for _, base := range []string{pf86, pf} {
		if base != "" {
			c = append(c, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	for _, base := range []string{pf, pf86, la} {
		if base != "" {
			c = append(c, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	for _, base := range []string{pf, la} {
		if base != "" {
			c = append(c, filepath.Join(base, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"))
		}
	}
	for _, p := range c {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	for _, n := range []string{"msedge", "chrome", "google-chrome", "chromium"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

func (a *App) openWindow(addr string) {
	u := fmt.Sprintf("http://%s/?k=%s", addr, a.token)
	b := findBrowser()
	if b == "" {
		openURL(u)
		return
	}
	prof := a.path("webview")
	args := []string{"--app=" + u, "--user-data-dir=" + prof, "--window-size=1180,700", "--no-first-run",
		"--no-default-browser-check", "--disable-sync", "--disable-features=Translate,msEdgeSidebarV2", "--hide-crash-restore-bubble"}
	c := exec.Command(b, args...)
	if err := c.Start(); err != nil {
		openURL(u)
		return
	}
	a.browser = c
}

// watchdog: когда окно закрыто и пингов нет, лаунчер завершается.
func (a *App) watchdog() {
	for {
		time.Sleep(3 * time.Second)
		a.mu.Lock()
		stale := a.pinged && time.Since(a.lastPing) > 25*time.Second
		a.mu.Unlock()
		if stale {
			a.shutdown()
		}
	}
}

type runInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func init() { runtime.LockOSThread() }

func main() {
	dir := os.Getenv("MOBILIX_DATA")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "."
		}
		dir = filepath.Join(base, "Mobilix")
	}
	_ = os.MkdirAll(dir, 0o755)
	a := &App{dataDir: dir, lg: newLogger()}
	a.loadSettings()

	// второй запуск: поднять окно уже работающего лаунчера и выйти
	if b, err := os.ReadFile(a.path("run.json")); err == nil {
		var ri runInfo
		if json.Unmarshal(b, &ri) == nil && ri.Port > 0 {
			if c, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(ri.Port), 600*time.Millisecond); err == nil {
				_ = c.Close()
				if !focusWindow() {
					a.token = ri.Token
					a.openWindow("127.0.0.1:" + itoa(ri.Port))
				}
				return
			}
		}
	}
	tk := make([]byte, 16)
	_, _ = rand.Read(tk)
	a.token = hex.EncodeToString(tk)
	ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(fixedPort))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	a.port = ln.Addr().(*net.TCPAddr).Port
	rb, _ := json.Marshal(runInfo{a.port, a.token})
	_ = os.WriteFile(a.path("run.json"), rb, 0o644)
	a.lg.add("[Mobilix] PC " + appVersion)
	go a.checkUpdate()
	srv := &http.Server{Handler: a.routes()}
	go func() { _ = srv.Serve(ln) }()
	addr := fmt.Sprintf("127.0.0.1:%d", a.port)
	if os.Getenv("MOBILIX_NOWINDOW") != "" {
		fmt.Printf("http://%s/?k=%s\n", addr, a.token)
		select {}
	}
	// собственное окно (WebView2 внутри exe); если не вышло, окно браузера в режиме приложения
	if a.runNative(fmt.Sprintf("http://%s/?k=%s", addr, a.token)) {
		a.shutdown()
	}
	a.openWindow(addr)
	a.watchdog()
}

func (a *App) checkUpdate() {
	c := &http.Client{Timeout: 8 * time.Second}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/Kumolix/Mobilix-Launcher-PC/releases/latest", nil)
	req.Header.Set("User-Agent", "Mobilix-PC/"+appVersion)
	resp, err := c.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&r) == nil && r.Tag != "" && strings.TrimPrefix(r.Tag, "v") != appVersion {
		a.updateTag = r.Tag
	}
}

// ---------- журнал ----------

type Logger struct {
	mu    sync.Mutex
	lines []string
	base  int
	file  *os.File
}

func newLogger() *Logger { return &Logger{} }

func (l *Logger) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 6000 {
		cut := len(l.lines) - 5000
		l.lines = append([]string(nil), l.lines[cut:]...)
		l.base += cut
	}
	if l.file != nil {
		_, _ = l.file.WriteString(s + "\n")
	}
}

func (l *Logger) since(n int) ([]string, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n < l.base {
		n = l.base
	}
	i := n - l.base
	if i > len(l.lines) {
		i = len(l.lines)
	}
	return append([]string(nil), l.lines[i:]...), l.base + len(l.lines)
}

// rotate: предыдущий запуск -> previous.log, новый latest.log.
func (a *App) rotateLog() {
	d := a.path("logs")
	_ = os.MkdirAll(d, 0o755)
	a.lg.mu.Lock()
	if a.lg.file != nil {
		_ = a.lg.file.Close()
	}
	_ = os.Remove(filepath.Join(d, "previous.log"))
	_ = os.Rename(filepath.Join(d, "latest.log"), filepath.Join(d, "previous.log"))
	f, _ := os.Create(filepath.Join(d, "latest.log"))
	a.lg.file = f
	a.lg.mu.Unlock()
}

func cleanNick(n string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(n) {
		if r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 16 {
		s = s[:16]
	}
	if len(s) < 3 {
		return "Player"
	}
	return s
}

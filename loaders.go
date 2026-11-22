package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type rawProf struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
}

func (a *App) rawProfile(id string) (rawProf, error) {
	var r rawProf
	b, err := os.ReadFile(a.verPath(id))
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(b, &r)
	return r, err
}

// ensureProfile скачивает описание версии (и загрузчика), возвращает id профиля для запуска.
func (a *App) ensureProfile(ctx context.Context, e Eff) (string, error) {
	a.setStage(a.tr("Список версий", "Version list"), 0, 0)
	if err := a.ensureVanillaJSON(ctx, e.MC); err != nil {
		return "", err
	}
	switch e.Loader {
	case "fabric", "quilt":
		return a.metaLoader(ctx, e)
	case "forge", "neoforge":
		return a.installerLoader(ctx, e)
	}
	return e.MC, nil
}

func (a *App) ensureVanillaJSON(ctx context.Context, mc string) error {
	if _, err := os.Stat(a.verPath(mc)); err == nil {
		return nil
	}
	m, err := a.manifest(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", a.tr("нет доступа к серверу версий", "cannot reach the version server"), err)
	}
	for _, ent := range m.Versions {
		if ent.ID == mc {
			return a.fetchFile(ctx, ent.URL, a.verPath(mc), ent.SHA1, 0, nil)
		}
	}
	return errors.New(a.tr("Версия не найдена: ", "Version not found: ") + mc)
}

// ---------- Fabric и Quilt ----------

func metaBase(loader string) string {
	if loader == "quilt" {
		return "https://meta.quiltmc.org/v3"
	}
	return "https://meta.fabricmc.net/v2"
}

func (a *App) metaVersions(ctx context.Context, loader, mc string) ([]string, error) {
	var list []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  *bool  `json:"stable"`
		} `json:"loader"`
	}
	if err := getJSON(ctx, metaBase(loader)+"/versions/loader/"+mc, &list); err != nil {
		return nil, err
	}
	var stable, all []string
	for _, e := range list {
		v := e.Loader.Version
		all = append(all, v)
		pre := strings.Contains(v, "beta") || strings.Contains(v, "alpha") || strings.Contains(v, "rc") || strings.Contains(v, "pre")
		if (e.Loader.Stable != nil && *e.Loader.Stable) || (e.Loader.Stable == nil && !pre) {
			stable = append(stable, v)
		}
	}
	if len(stable) > 0 {
		return stable, nil
	}
	if len(all) == 0 {
		return nil, fmt.Errorf(a.tr("Для версии %s нет загрузчика %s", "No %[2]s for version %[1]s"), mc, loaderLabel(loader))
	}
	return all, nil
}

func (a *App) metaLoader(ctx context.Context, e Eff) (string, error) {
	prefix := e.Loader + "-loader-"
	lv := e.LoaderVer
	if lv == "" {
		l, err := a.metaVersions(ctx, e.Loader, e.MC)
		if err != nil {
			// без сети подходит уже установленный загрузчик
			for _, id := range a.installedVersions() {
				if strings.HasPrefix(id, prefix) && strings.HasSuffix(id, "-"+e.MC) {
					return id, nil
				}
			}
			return "", err
		}
		lv = l[0]
	}
	id := prefix + lv + "-" + e.MC
	if _, err := os.Stat(a.verPath(id)); err == nil {
		return id, nil
	}
	u := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", metaBase(e.Loader), e.MC, lv)
	if err := a.fetchFile(ctx, u, a.verPath(id), "", 0, nil); err != nil {
		return "", err
	}
	return id, nil
}

// ---------- версии загрузчиков ----------

func verKey(v string) []int {
	var out []int
	for _, p := range regexp.MustCompile(`[.\-]`).Split(v, -1) {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = -1
		}
		out = append(out, n)
	}
	return out
}

func verLess(a, b string) bool {
	x, y := verKey(a), verKey(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p < q
		}
	}
	return false
}

func sortDesc(v []string) {
	sort.Slice(v, func(i, j int) bool { return verLess(v[j], v[i]) })
}

func uniq(v []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range v {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func getText(ctx context.Context, u string) (string, error) {
	var last error
	for _, x := range urlsWithMirrors(u) {
		b, err := getBytes(ctx, x)
		if err == nil {
			return string(b), nil
		}
		last = err
	}
	return "", last
}

var verTag = regexp.MustCompile(`<version>([^<]+)</version>`)

func (a *App) forgeVersions(ctx context.Context, mc string) []string {
	var out []string
	var promos struct {
		Promos map[string]string `json:"promos"`
	}
	if getJSON(ctx, "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", &promos) == nil {
		out = append(out, promos.Promos[mc+"-recommended"], promos.Promos[mc+"-latest"])
	}
	var maven []string
	if xml, err := getText(ctx, "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"); err == nil {
		for _, m := range verTag.FindAllStringSubmatch(xml, -1) {
			if strings.HasPrefix(m[1], mc+"-") {
				maven = append(maven, strings.TrimPrefix(m[1], mc+"-"))
			}
		}
	}
	if len(maven) == 0 {
		var arr []struct {
			Version string `json:"version"`
		}
		if getJSON(ctx, "https://bmclapi2.bangbang93.com/forge/minecraft/"+mc, &arr) == nil {
			for _, x := range arr {
				maven = append(maven, x.Version)
			}
		}
	}
	sortDesc(maven)
	return uniq(append(out, maven...))
}

func neoMC(v string) string {
	p := strings.Split(strings.SplitN(v, "-", 2)[0], ".")
	if len(p) < 2 {
		return ""
	}
	a, e1 := strconv.Atoi(p[0])
	b, e2 := strconv.Atoi(p[1])
	if e1 != nil || e2 != nil {
		return ""
	}
	if a < 26 {
		if b > 0 {
			return fmt.Sprintf("1.%d.%d", a, b)
		}
		return fmt.Sprintf("1.%d", a)
	}
	return fmt.Sprintf("%d.%d", a, b)
}

func (a *App) neoforgeVersions(ctx context.Context, mc string) []string {
	var out []string
	if xml, err := getText(ctx, "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"); err == nil {
		for _, m := range verTag.FindAllStringSubmatch(xml, -1) {
			if neoMC(m[1]) == mc {
				out = append(out, m[1])
			}
		}
	}
	if len(out) == 0 {
		var arr []struct {
			Version string `json:"version"`
		}
		if getJSON(ctx, "https://bmclapi2.bangbang93.com/neoforge/list/"+mc, &arr) == nil {
			for _, x := range arr {
				out = append(out, strings.TrimPrefix(x.Version, "neoforge-"))
			}
		}
	}
	sortDesc(out)
	return uniq(out)
}

func (a *App) loaderVersions(ctx context.Context, loader, mc string) ([]string, error) {
	var out []string
	var err error
	switch loader {
	case "fabric", "quilt":
		out, err = a.metaVersions(ctx, loader, mc)
	case "forge":
		out = a.forgeVersions(ctx, mc)
	case "neoforge":
		out = a.neoforgeVersions(ctx, mc)
	default:
		return nil, errors.New("unknown loader")
	}
	if err == nil && len(out) == 0 {
		err = fmt.Errorf(a.tr("Для версии %s нет загрузчика %s", "No %[2]s for version %[1]s"), mc, loaderLabel(loader))
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out, err
}

// ---------- Forge и NeoForge: запуск установщика без окна ----------

func (a *App) findLoaderProfile(loader, mc, ver string) string {
	var best string
	var bestT time.Time
	for _, id := range a.installedVersions() {
		if loaderOfProfile(id) != loader {
			continue
		}
		r, err := a.rawProfile(id)
		if err != nil || (r.InheritsFrom != "" && r.InheritsFrom != mc) {
			continue
		}
		if r.InheritsFrom == "" && !strings.Contains(id, mc) {
			continue
		}
		if ver != "" && !strings.Contains(id, ver) {
			continue
		}
		if st, err := os.Stat(a.verPath(id)); err == nil && st.ModTime().After(bestT) {
			best, bestT = id, st.ModTime()
		}
	}
	return best
}

func installerURLs(loader, mc, v string) []string {
	if loader == "neoforge" {
		return []string{fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%[1]s/neoforge-%[1]s-installer.jar", v)}
	}
	var out []string
	for _, suf := range []string{"", "-" + mc, "-" + mc + ".0"} {
		full := mc + "-" + v + suf
		out = append(out, fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%[1]s/forge-%[1]s-installer.jar", full))
	}
	return out
}

func (a *App) installerLoader(ctx context.Context, e Eff) (string, error) {
	if id := a.findLoaderProfile(e.Loader, e.MC, e.LoaderVer); id != "" && a.profileComplete(id) {
		return id, nil
	}
	var cands []string
	if e.LoaderVer != "" {
		cands = []string{e.LoaderVer}
	} else {
		l, err := a.loaderVersions(ctx, e.Loader, e.MC)
		if err != nil {
			if id := a.findLoaderProfile(e.Loader, e.MC, ""); id != "" {
				return id, nil
			}
			return "", err
		}
		if len(l) > 6 {
			l = l[:6]
		}
		cands = l
	}
	// установщик требует скачанный клиент игры
	s := a.settings()
	v, err := a.loadVersion(e.MC)
	if err != nil {
		return "", err
	}
	if err := a.ensureFiles(ctx, v, s); err != nil {
		return "", err
	}
	req := 8
	if v.JavaVersion != nil {
		req = v.JavaVersion.MajorVersion
	}
	java, err := a.ensureJava(ctx, req, s)
	if err != nil {
		return "", err
	}
	inst := a.path("cache", "installer-"+e.Loader+".jar")
	var got string
	var last error
	for _, cv := range cands {
		a.setStage(fmt.Sprintf(a.tr("%s %s: установщик", "%s %s: installer"), loaderLabel(e.Loader), cv), 0, 0)
		_ = os.Remove(inst)
		ok := false
		for _, u := range installerURLs(e.Loader, e.MC, cv) {
			if err := a.fetchFile(ctx, u, inst, "", 0, nil); err == nil {
				if st, e2 := os.Stat(inst); e2 == nil && st.Size() > 100000 {
					ok = true
					break
				}
			} else {
				last = err
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
		}
		if ok {
			got = cv
			break
		}
	}
	if got == "" {
		if last == nil {
			last = errors.New("installer not found")
		}
		return "", fmt.Errorf("%s: %w", a.tr("не удалось скачать установщик", "could not download the installer"), last)
	}
	root := a.path("shared")
	if _, err := os.Stat(filepath.Join(root, "launcher_profiles.json")); err != nil {
		_ = os.WriteFile(filepath.Join(root, "launcher_profiles.json"), []byte(`{"profiles":{}}`), 0o644)
	}
	a.setStage(fmt.Sprintf(a.tr("%s %s: установка (это долго)", "%s %s: installing (this takes a while)"), loaderLabel(e.Loader), got), 0, 0)
	a.lg.add(fmt.Sprintf("[Mobilix] %s %s: %s", loaderLabel(e.Loader), got, a.tr("запускаю установщик", "running the installer")))
	cmd := exec.CommandContext(ctx, java, "-Djava.awt.headless=true", "-jar", inst, "--installClient", root)
	cmd.Dir = root
	hideWindow(cmd)
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		a.lg.add("[installer] " + sc.Text())
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%s: %w", a.tr("установщик завершился с ошибкой (подробности в логе)", "the installer failed (see the log)"), err)
	}
	id := a.findLoaderProfile(e.Loader, e.MC, got)
	if id == "" {
		id = a.findLoaderProfile(e.Loader, e.MC, "")
	}
	if id == "" {
		return "", errors.New(a.tr("Установщик не создал профиль версии", "The installer did not create a version profile"))
	}
	return id, nil
}

// profileComplete: профиль Forge создаётся раньше, чем постпроцессоры: проверяем, что клиентские файлы на месте.
func (a *App) profileComplete(id string) bool {
	v, err := a.loadVersion(id)
	if err != nil {
		return false
	}
	libs, _ := a.collectLibs(v)
	for _, l := range libs {
		if l.art.URL == "" && l.art.Path != "" {
			if _, err := os.Stat(a.path("shared", "libraries", filepath.FromSlash(l.art.Path))); err != nil {
				return false
			}
		}
	}
	return true
}

// ---------- OptiFine (через BMCLAPI) ----------

type ofBuild struct {
	MC    string `json:"mcversion"`
	Type  string `json:"type"`
	Patch string `json:"patch"`
	File  string `json:"filename"`
}

func (a *App) optifineList(ctx context.Context, mc string) ([]ofBuild, error) {
	var list []ofBuild
	if err := getJSON(ctx, "https://bmclapi2.bangbang93.com/optifine/"+mc, &list); err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Patch > list[j].Patch })
	return list, nil
}

func (a *App) optifineInstall(ctx context.Context, e Eff, b ofBuild) (string, error) {
	dir := filepath.Join(a.gameDirE(e), "mods")
	name := b.File
	if name == "" {
		name = fmt.Sprintf("OptiFine_%s_%s_%s.jar", b.MC, b.Type, b.Patch)
	}
	u := fmt.Sprintf("https://bmclapi2.bangbang93.com/optifine/%s/%s/%s", b.MC, b.Type, b.Patch)
	if err := a.fetchFile(ctx, u, filepath.Join(dir, filepath.Base(name)), "", 0, nil); err != nil {
		return "", err
	}
	return filepath.Base(name), nil
}

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) launch(ctx context.Context, v *Version, s Settings, e Eff, java string) error {
	a.setStage(a.tr("Запуск игры", "Starting the game"), 0, 0)
	name, uuid, token, utype := a.playerName(s)
	libs, _ := a.collectLibs(v)
	var cp []string
	for _, l := range libs {
		cp = append(cp, a.path("shared", "libraries", filepath.FromSlash(l.art.Path)))
	}
	primary := a.path("shared", "versions", v.jarID, v.jarID+".jar")
	cp = append(cp, primary)
	gdir := a.gameDirE(e)
	_ = os.MkdirAll(gdir, 0o755)
	assetsID := v.Assets
	if v.AssetIndex != nil {
		assetsID = v.AssetIndex.ID
	}
	gameAssets := a.assetsRoot()
	if assetsID == "legacy" {
		gameAssets = filepath.Join(a.assetsRoot(), "virtual", "legacy")
	} else if assetsID == "pre-1.6" {
		gameAssets = filepath.Join(gdir, "resources")
	}
	vtype := v.Type
	if vtype == "" {
		vtype = "release"
	}

	a.applyGraphics(gdir, e.MC, s)
	if slim, on := a.applySkinPack(ctx, gdir, e.MC, s); on && utype == "legacy" {
		uuid = skinUUID(name, slim) // UUID с нужной шириной рук стандартного скина
	}

	// Ely.by: authlib-injector подменяет сервера авторизации
	var agent []string
	if utype == "mojang" {
		if !a.elyEnsure(ctx) {
			return errors.New(a.tr("Сессия Ely.by истекла — войдите снова", "Ely.by session expired — sign in again"))
		}
		_, _, token, _ = a.playerName(s)
		jar, err := a.ensureAuthlib(ctx)
		if err != nil {
			return err
		}
		agent = []string{"-javaagent:" + jar + "=https://authserver.ely.by/api/authlib-injector"}
	}

	cl, mappings := a.corelixPlan(ctx, v, s, e)
	mainClass := v.MainClass
	if cl != nil {
		cp = append(cp, cl.loader())
	}

	sep := string(os.PathListSeparator)
	repl := map[string]string{
		"auth_player_name": name, "version_name": v.jarID, // Forge: -DignoreList=...,${version_name}.jar — имя jar клиента (иначе он грузится как модуль и ломает запуск) "game_directory": gdir, "assets_root": a.assetsRoot(),
		"assets_index_name": assetsID, "auth_uuid": uuid, "auth_access_token": token, "clientid": "", "auth_xuid": "",
		"user_type": utype, "version_type": vtype, "user_properties": "{}", "auth_session": token, "game_assets": gameAssets,
		"natives_directory": a.nativesDir(v), "launcher_name": "Mobilix", "launcher_version": appVersion,
		"classpath": strings.Join(cp, sep), "library_directory": a.path("shared", "libraries"),
		"classpath_separator": sep, "primary_jar": primary,
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

	ram := s.RamMB
	if s.MemAuto || ram <= 0 {
		ram = autoRAM()
	}
	args := []string{fmt.Sprintf("-Xmx%dM", ram), "-Xms256M", "-Dlog4j2.formatMsgNoLookups=true"}
	args = append(args, agent...)
	jm := javaMajor(java)
	switch s.GC {
	case "parallel":
		args = append(args, "-XX:+UseParallelGC")
	case "serial":
		args = append(args, "-XX:+UseSerialGC")
	case "zgc":
		if jm >= 15 {
			args = append(args, "-XX:+UseZGC")
		}
	default:
		if jm >= 9 || jm == 0 {
			args = append(args, "-XX:+UseG1GC")
		}
	}
	if cl != nil {
		args = append(args,
			"-Dcorelix.platform=desktop", "-Dcorelix.libs="+cl.libs(), "-Dcorelix.mods="+cl.mods(),
			"-Dcorelix.mappings="+mappings, "-Dcorelix.cache="+a.path("cache", "corelix"),
			"-Dcorelix.mainClass="+v.MainClass)
		_ = os.MkdirAll(a.path("cache", "corelix"), 0o755)
		mainClass = corelixMain
		a.lg.add("[Mobilix] Corelix " + cl.tag)
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
	if s.JoinNext && strings.TrimSpace(s.JoinIP) != "" {
		ip := strings.TrimSpace(s.JoinIP)
		if mcMinor(e.MC) >= 20 {
			game = append(game, "--quickPlayMultiplayer", ip)
		} else {
			host, port := ip, ""
			if i := strings.LastIndex(ip, ":"); i > 0 && strings.Count(ip, ":") == 1 {
				host, port = ip[:i], ip[i+1:]
			}
			game = append(game, "--server", host)
			if port != "" {
				game = append(game, "--port", port)
			}
		}
		a.lg.add("[Mobilix] " + a.tr("Подключение к серверу: ", "Joining server: ") + ip)
		a.saveSettings(func(x *Settings) { x.JoinNext = false }) // только на один запуск
	}
	full := append(append(append(args, jvm...), mainClass), game...)
	a.lg.add("[Mobilix] Java: " + java)
	a.lg.add("[Mobilix] " + a.tr("Папка игры: ", "Game folder: ") + gdir)
	cmd := exec.Command(gameJava(java), full...)
	cmd.Dir = gdir
	hideWindow(cmd)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	started := time.Now()
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
		if s.WorldBackup {
			key := e.Key
			if key == "" {
				key = "game"
			}
			a.backupWorlds(gdir, key, started)
		}
	}()
	if s.CloseOnPlay {
		go func() { time.Sleep(4 * time.Second); a.shutdown() }()
	}
	return nil
}

// autoRAM: половина физической памяти, но не меньше 2 ГБ и не больше 8 ГБ.
func autoRAM() int {
	mb := totalRAMMB()
	r := mb / 2
	if r < 2048 {
		r = 2048
	}
	if r > 8192 {
		r = 8192
	}
	return r
}

package main

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Ресурспак со своим скином: подменяет все стандартные скины и сам включается в options.txt.
// Для игры без аккаунта это единственный способ показать свой скин.

func (a *App) customSkinPath() string { return a.path("skins", "custom.png") }

// packFormat — pack_format ресурспаков по версии игры.
func packFormat(mc string) int {
	minor, patch := mcMinor(mc), 0
	if m := regexp.MustCompile(`^1\.\d+\.(\d+)`).FindStringSubmatch(mc); m != nil {
		patch, _ = strconv.Atoi(m[1])
	}
	switch {
	case minor <= 8:
		return 1
	case minor <= 10:
		return 2
	case minor <= 12:
		return 3
	case minor <= 14:
		return 4
	case minor == 15:
		return 5
	case minor == 16:
		if patch >= 2 {
			return 6
		}
		return 5
	case minor == 17:
		return 7
	case minor == 18:
		return 8
	case minor == 19:
		if patch >= 4 {
			return 13
		} else if patch == 3 {
			return 12
		}
		return 9
	case minor == 20:
		switch {
		case patch >= 5:
			return 32
		case patch >= 3:
			return 22
		case patch == 2:
			return 18
		}
		return 15
	case minor == 21:
		switch {
		case patch >= 11:
			return 75
		case patch >= 9:
			return 69
		case patch >= 7:
			return 64
		case patch == 6:
			return 63
		case patch == 5:
			return 55
		case patch == 4:
			return 46
		case patch >= 2:
			return 42
		}
		return 34
	}
	return 75
}

// packSkin выбирает скин, который нужно показать в игре через ресурспак:
// свой файл или скин по нику (для игры без аккаунта). С Ely.by скин подставляет authlib-injector.
func (a *App) packSkin(ctx context.Context, s Settings) (png []byte, slim, ok bool) {
	if a.account().Type == "ely" {
		return nil, false, false
	}
	switch s.SkinSrc {
	case "file":
		b, err := os.ReadFile(a.customSkinPath())
		return b, s.SkinModel == 2, err == nil
	case "mojang", "":
		nick := s.SkinNick
		if nick == "" {
			nick, _, _, _ = a.playerName(s)
		}
		nick = cleanNick(nick)
		if nick == "" {
			return nil, false, false
		}
		cache := a.path("skins", "mojang_"+strings.ToLower(nick)+".png")
		fctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		b, model, err := fetchMojangSkin(fctx, nick)
		if err == nil {
			_ = os.MkdirAll(a.path("skins"), 0o755)
			_ = os.WriteFile(cache, b, 0o644)
			_ = os.WriteFile(cache+".model", []byte(model), 0o644)
		} else if b, err = os.ReadFile(cache); err == nil {
			m, _ := os.ReadFile(cache + ".model")
			model = string(m)
		} else {
			return nil, false, false
		}
		return b, model == "slim", true
	}
	return nil, false, false
}

func (a *App) applySkinPack(ctx context.Context, gdir, mc string, s Settings) (slim, active bool) {
	const packName = "Mobilix-skin.zip"
	custom, slim, want := a.packSkin(ctx, s)
	oldNames := mcMinor(mc) <= 12 // до 1.13 в options.txt имена паков без «file/»
	entry := "file/" + packName
	if oldNames {
		entry = packName
	}
	packFile := filepath.Join(gdir, "resourcepacks", packName)
	opt := filepath.Join(gdir, "options.txt")
	var lines []string
	if b, err := os.ReadFile(opt); err == nil {
		lines = strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	}
	idx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "resourcePacks:") {
			idx = i
		}
	}
	var list []string
	if idx >= 0 {
		_ = json.Unmarshal([]byte(strings.TrimPrefix(lines[idx], "resourcePacks:")), &list)
	}
	has := func(x string) int {
		for i, v := range list {
			if v == x {
				return i
			}
		}
		return -1
	}
	if want {
		_ = os.MkdirAll(filepath.Dir(packFile), 0o755)
		f, err := os.Create(packFile)
		if err != nil {
			a.lg.add("[Mobilix] " + a.tr("Скин в игре: ", "In-game skin: ") + err.Error())
			return slim, false
		}
		zw := zip.NewWriter(f)
		put := func(name string, data []byte) {
			w, _ := zw.Create(name)
			_, _ = w.Write(data)
		}
		pf := packFormat(mc)
		meta := fmt.Sprintf(`{"pack":{"pack_format":%d,"description":"Mobilix skin"}}`, pf)
		if mcMinor(mc) >= 20 && pf >= 18 { // широкий диапазон: пак не помечается как несовместимый
			meta = fmt.Sprintf(`{"pack":{"pack_format":%d,"supported_formats":{"min_inclusive":%d,"max_inclusive":%d},"description":"Mobilix skin"}}`, pf, 15, 1000)
		}
		put("pack.mcmeta", []byte(meta))
		for _, n := range []string{"steve", "alex"} { // 1.8–1.19.2
			put("assets/minecraft/textures/entity/"+n+".png", custom)
		}
		for _, n := range []string{"alex", "ari", "efe", "kai", "makena", "noor", "steve", "sunny", "zuri"} {
			for _, d := range []string{"wide", "slim"} {
				put("assets/minecraft/textures/entity/player/"+d+"/"+n+".png", custom)
			}
		}
		_ = zw.Close()
		_ = f.Close()
		if !oldNames && has("vanilla") < 0 {
			list = append([]string{"vanilla"}, list...)
		}
		if has(entry) < 0 {
			list = append(list, entry)
		}
	} else {
		if i := has(entry); i >= 0 {
			list = append(list[:i], list[i+1:]...)
		} else if _, err := os.Stat(packFile); err != nil {
			return slim, false // нечего менять
		}
		_ = os.Remove(packFile)
	}
	b, _ := json.Marshal(list)
	nl := "resourcePacks:" + string(b)
	if idx >= 0 {
		lines[idx] = nl
	} else {
		lines = append(lines, nl)
	}
	_ = os.WriteFile(opt, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	if want {
		a.lg.add("[Mobilix] " + a.tr("Скин включён ресурспаком Mobilix-skin", "Skin enabled via the Mobilix-skin resource pack"))
	}
	return slim, want
}

// skinUUID: при своём скине подбираем UUID, у которого стандартный скин нужной ширины (иначе руки не совпадут).
func skinUUID(nick string, slim bool) string {
	for k := 0; k < 500; k++ {
		h := md5.Sum([]byte("OfflinePlayer:" + nick + "#" + strconv.Itoa(k)))
		h[6] = (h[6] & 0x0f) | 0x30
		h[8] = (h[8] & 0x3f) | 0x80
		msb, lsb := binary.BigEndian.Uint64(h[:8]), binary.BigEndian.Uint64(h[8:])
		hilo := msb ^ lsb
		hash := int32(int32(hilo>>32) ^ int32(hilo)) // UUID.hashCode() в Java
		idx := ((int(hash) % 18) + 18) % 18
		if (idx < 9) == slim {
			x := hex.EncodeToString(h[:])
			return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
		}
	}
	return offlineUUID(nick)
}

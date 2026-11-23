package main

import (
	"archive/zip"
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
)

// Ресурспак со своим скином: подменяет все стандартные скины и сам включается в options.txt.
// Для игры без аккаунта это единственный способ показать свой скин.

func (a *App) customSkinPath() string { return a.path("skins", "custom.png") }

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
		return 9
	}
	return 42
}

func (a *App) applySkinPack(gdir, mc string, s Settings) {
	const packName = "Mobilix-skin.zip"
	custom, err := os.ReadFile(a.customSkinPath())
	want := err == nil && s.SkinSrc == "file" && a.account().Type != "ely"
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
			return
		}
		zw := zip.NewWriter(f)
		put := func(name string, data []byte) {
			w, _ := zw.Create(name)
			_, _ = w.Write(data)
		}
		put("pack.mcmeta", []byte(fmt.Sprintf(`{"pack":{"pack_format":%d,"description":"Mobilix skin"}}`, packFormat(mc))))
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
			return // нечего менять
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
		a.lg.add("[Mobilix] " + a.tr("Свой скин включён ресурспаком", "Custom skin enabled via resource pack"))
	}
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

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Быстрые настройки графики: записываются в options.txt один раз при выборе, FPS и мипмапы — при каждом запуске.
type Preset struct {
	RU, EN    string
	FPS       int
	Chunks    int
	AO, Fancy bool
	Mip       int
	VBO       bool
	Clouds    bool
	Particles int
}

var presets = []Preset{
	{"Минимальные", "Minimal", 30, 2, false, false, 0, false, false, 2},
	{"Низкие", "Low", 45, 6, false, false, 0, false, false, 2},
	{"Средние", "Medium", 60, 12, true, false, 2, false, true, 1},
	{"Высокие", "High", 0, 28, true, true, 4, true, true, 0},
}

func mcMinor(ver string) int {
	p := strings.Split(ver, ".")
	if len(p) > 1 {
		if n, err := strconv.Atoi(p[1]); err == nil {
			return n
		}
	}
	return 12
}

func boolS(b bool) string { return strconv.FormatBool(b) }

// setOptions обновляет ключи в options.txt, остальные строки сохраняет.
func setOptions(gdir string, kv map[string]string, order []string) {
	opt := filepath.Join(gdir, "options.txt")
	var keep []string
	if b, err := os.ReadFile(opt); err == nil {
		for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if l == "" {
				continue
			}
			k := strings.SplitN(l, ":", 2)[0]
			if _, ok := kv[k]; !ok {
				keep = append(keep, l)
			}
		}
	}
	for _, k := range order {
		keep = append(keep, k+":"+kv[k])
	}
	_ = os.WriteFile(opt, []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

func (a *App) applyGraphics(gdir, mc string, s Settings) {
	minor := mcMinor(mc)
	marker := filepath.Join(gdir, ".mobilix-preset")
	if s.Preset >= 1 && s.Preset <= len(presets) {
		stamp := strconv.Itoa(s.Preset) + "-" + strconv.Itoa(s.PresetStamp)
		if b, _ := os.ReadFile(marker); string(b) != stamp {
			pr := presets[s.Preset-1]
			kv := map[string]string{}
			var ord []string
			set := func(k, v string) { kv[k] = v; ord = append(ord, k) }
			chunks := pr.Chunks
			if minor <= 8 && chunks > 16 {
				chunks = 16
			}
			set("renderDistance", strconv.Itoa(chunks))
			set("enableVsync", "false")
			fps := pr.FPS
			if fps <= 0 {
				fps = 260
			}
			set("maxFps", strconv.Itoa(fps))
			set("mipmapLevels", strconv.Itoa(pr.Mip))
			if minor >= 14 {
				set("ao", boolS(pr.AO))
			} else if pr.AO {
				set("ao", "2")
			} else {
				set("ao", "0")
			}
			set("fancyGraphics", boolS(pr.Fancy))
			if minor >= 16 {
				if pr.Fancy {
					set("graphicsMode", "1")
				} else {
					set("graphicsMode", "0")
				}
			}
			if minor <= 15 {
				set("useVbo", boolS(pr.VBO))
			}
			if minor <= 8 {
				set("clouds", boolS(pr.Clouds))
			} else {
				set("renderClouds", boolS(pr.Clouds))
			}
			set("particles", strconv.Itoa(pr.Particles))
			weak := pr.Chunks <= 12
			if minor >= 18 {
				sim := "12"
				if pr.Chunks <= 6 {
					sim = "5"
				} else if weak {
					sim = "8"
				}
				set("simulationDistance", sim)
			}
			if minor >= 16 {
				ent := "1.0"
				if pr.Chunks <= 6 {
					ent = "0.5"
				} else if weak {
					ent = "0.75"
				}
				set("entityDistanceScaling", ent)
			}
			if minor >= 21 {
				if weak {
					set("chunkSectionFadeInTime", "0.0")
				} else {
					set("chunkSectionFadeInTime", "0.75")
				}
			}
			set("entityShadows", boolS(pr.Fancy))
			if minor >= 15 {
				b := "0"
				if pr.Fancy {
					b = "2"
				} else if pr.AO {
					b = "1"
				}
				set("biomeBlendRadius", b)
			}
			setOptions(gdir, kv, ord)
			_ = os.WriteFile(marker, []byte(stamp), 0o644)
			a.lg.add("[Mobilix] " + a.tr("Быстрые настройки графики: ", "Quick graphics settings: ") + pr.RU)
		}
	}
	kv, ord := map[string]string{}, []string{}
	if s.MaxFps > 0 {
		kv["maxFps"] = strconv.Itoa(s.MaxFps)
		ord = append(ord, "maxFps")
	}
	if s.Mip >= 0 {
		kv["mipmapLevels"] = strconv.Itoa(s.Mip)
		ord = append(ord, "mipmapLevels")
	}
	if len(ord) > 0 {
		setOptions(gdir, kv, ord)
	}
}

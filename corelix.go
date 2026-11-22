package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Corelix — загрузчик с оптимизациями для клиента (Kumolix/OptiCore). Подключается к чистой ванильной версии из списка.
const (
	corelixRepo   = "Kumolix/OptiCore"
	corelixMain   = "dev.corelix.loader.CorelixMain"
	corelixBundle = "corelix-bundle.zip"
)

var corelixSupported = map[string]bool{"1.21.3": true}

type corelix struct {
	dir, tag string
}

func (c *corelix) loader() string { return filepath.Join(c.dir, "corelix-loader.jar") }
func (c *corelix) libs() string   { return filepath.Join(c.dir, "libs") }
func (c *corelix) mods() string   { return filepath.Join(c.dir, "mods") }

func (a *App) corelixDir() string { return a.path("corelix") }

func (a *App) corelixCached() *corelix {
	b, err := os.ReadFile(filepath.Join(a.corelixDir(), "tag.txt"))
	if err != nil {
		return nil
	}
	tag := strings.TrimSpace(string(b))
	c := &corelix{filepath.Join(a.corelixDir(), "bundle-"+tag), tag}
	if _, err := os.Stat(filepath.Join(c.dir, "READY")); err != nil {
		return nil
	}
	if _, err := os.Stat(c.loader()); err != nil {
		return nil
	}
	return c
}

// ensureCorelix берёт последний релиз; без сети или при сбое — ранее скачанный. nil: Corelix недоступен.
func (a *App) ensureCorelix(ctx context.Context) *corelix {
	cached := a.corelixCached()
	var rel struct {
		Tag    string `json:"tag_name"`
		Draft  bool   `json:"draft"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := getJSON(ctx, "https://api.github.com/repos/"+corelixRepo+"/releases/latest", &rel); err != nil || rel.Tag == "" || rel.Draft {
		if cached == nil {
			a.lg.add("[Mobilix] " + a.tr("Corelix: не удалось получить релиз, запускаю ванильную игру.", "Corelix: could not get a release, launching vanilla."))
		}
		return cached
	}
	if cached != nil && cached.tag == rel.Tag {
		return cached
	}
	var zipURL, shaURL string
	for _, x := range rel.Assets {
		switch x.Name {
		case corelixBundle:
			zipURL = x.URL
		case corelixBundle + ".sha256":
			shaURL = x.URL
		}
	}
	if zipURL == "" {
		return cached
	}
	a.lg.add("[Mobilix] " + a.tr("Corelix: скачиваю релиз ", "Corelix: downloading release ") + rel.Tag)
	tmpZip := filepath.Join(a.path("cache"), "corelix-bundle.zip")
	if err := a.fetchFile(ctx, zipURL, tmpZip, "", 0, nil); err != nil {
		a.lg.add("[Mobilix] Corelix: " + err.Error())
		return cached
	}
	if shaURL != "" {
		want, err := getText(ctx, shaURL)
		if err == nil {
			if got := fileSHA256(tmpZip); !strings.HasPrefix(strings.ToLower(strings.TrimSpace(want)), got) {
				a.lg.add("[Mobilix] Corelix: " + a.tr("контрольная сумма не совпала, использую прежнюю версию", "checksum mismatch, keeping the previous version"))
				_ = os.Remove(tmpZip)
				return cached
			}
		}
	}
	dir := filepath.Join(a.corelixDir(), "bundle-"+rel.Tag)
	_ = os.RemoveAll(dir)
	if err := unzip(tmpZip, dir, nil); err != nil {
		_ = os.RemoveAll(dir)
		a.lg.add("[Mobilix] Corelix: " + err.Error())
		return cached
	}
	_ = os.Remove(tmpZip)
	c := &corelix{dir, rel.Tag}
	if _, err := os.Stat(c.loader()); err != nil {
		_ = os.RemoveAll(dir)
		return cached
	}
	_ = os.WriteFile(filepath.Join(dir, "READY"), []byte("ok"), 0o644)
	_ = os.WriteFile(filepath.Join(a.corelixDir(), "tag.txt"), []byte(rel.Tag), 0o644)
	a.lg.add("[Mobilix] Corelix " + rel.Tag + " " + a.tr("установлен", "installed"))
	return c
}

func fileSHA256(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// corelixPlan: нужен ли Corelix для этого запуска и откуда взять маппинги клиента.
func (a *App) corelixPlan(ctx context.Context, v *Version, s Settings, e Eff) (*corelix, string) {
	if !s.Corelix || (e.Loader != "" && e.Loader != "vanilla") || !corelixSupported[e.MC] || v.ID != e.MC {
		return nil, ""
	}
	m, ok := v.Downloads["client_mappings"]
	if !ok || m.URL == "" {
		a.lg.add("[Mobilix] " + a.tr("Corelix: у версии нет маппингов, пропускаю.", "Corelix: no mappings for this version, skipping."))
		return nil, ""
	}
	mp := a.path("shared", "versions", e.MC, "client_mappings.txt")
	if err := a.fetchFile(ctx, m.URL, mp, m.SHA1, 0, nil); err != nil {
		a.lg.add("[Mobilix] " + a.tr("Corelix: не скачались маппинги, запускаю ванильную игру.", "Corelix: mappings failed, launching vanilla."))
		return nil, ""
	}
	c := a.ensureCorelix(ctx)
	if c == nil {
		return nil, ""
	}
	return c, mp
}

var _ = zip.ErrFormat
var _ = json.Marshal
var _ = errors.New

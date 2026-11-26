package main

import (
	"bytes"
	"context"
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
	"strings"
	"time"
)

// Игра по сети поверх установленного ZeroTier One (виртуальная локальная сеть, как Radmin).
// Лаунчер управляет им через локальный API службы: вход в сети, выход, адрес в сети, порт мира для друзей.

type ZtNet struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func ztDir() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "ZeroTier", "One")
	}
	return "/var/lib/zerotier-one"
}

func ztToken() string {
	for _, p := range []string{filepath.Join(ztDir(), "authtoken.secret")} {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(h, ".zeroTierOneAuthToken")); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

func ztPort() string {
	if b, err := os.ReadFile(filepath.Join(ztDir(), "zerotier-one.port")); err == nil {
		if p := strings.TrimSpace(string(b)); p != "" {
			return p
		}
	}
	return "9993"
}

func ztInstalled() bool {
	if _, err := os.Stat(ztDir()); err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		for _, e := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if d := os.Getenv(e); d != "" {
				if _, err := os.Stat(filepath.Join(d, "ZeroTier", "One")); err == nil {
					return true
				}
			}
		}
	}
	return false
}

func ztCall(ctx context.Context, method, path string, body any, out any) error {
	tok := ztToken()
	if tok == "" {
		return fmt.Errorf("no token")
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:"+ztPort()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-ZT1-Auth", tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type ztNetInfo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	IPs    []string `json:"ips"`
}

type ztState struct {
	Installed bool        `json:"installed"`
	Running   bool        `json:"running"`
	Online    bool        `json:"online"`
	Address   string      `json:"address"`
	Version   string      `json:"version"`
	Nets      []ztNetInfo `json:"nets"`
	Port      int         `json:"port"`
	Joined    bool        `json:"joined"`
}

func (a *App) ztState(ctx context.Context) ztState {
	st := ztState{Installed: ztInstalled()}
	var s struct {
		Address string `json:"address"`
		Online  bool   `json:"online"`
		Version string `json:"version"`
	}
	if err := ztCall(ctx, "GET", "/status", nil, &s); err != nil {
		return st
	}
	st.Running, st.Online, st.Address, st.Version = true, s.Online, s.Address, s.Version
	var nets []struct {
		ID    string   `json:"nwid"`
		Name  string   `json:"name"`
		State string   `json:"status"`
		Addrs []string `json:"assignedAddresses"`
	}
	_ = ztCall(ctx, "GET", "/network", nil, &nets)
	saved := a.settings().ZtNets
	for _, n := range nets {
		info := ztNetInfo{ID: n.ID, Name: n.Name, Status: n.State}
		for _, ad := range n.Addrs {
			if ip := strings.Split(ad, "/")[0]; strings.Count(ip, ".") == 3 {
				info.IPs = append(info.IPs, ip)
			}
		}
		if info.Name == "" {
			for _, sv := range saved {
				if sv.ID == n.ID {
					info.Name = sv.Name
				}
			}
		}
		st.Nets = append(st.Nets, info)
		st.Joined = true
	}
	st.Port = a.lanPort()
	return st
}

// lanPort: порт открытого для сети мира; Minecraft пишет «Started serving on N» в latest.log.
func (a *App) lanPort() int {
	for _, p := range []string{filepath.Join(a.gameDir(a.settings()), "logs", "latest.log"), a.path("latest.log")} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		fi, _ := f.Stat()
		start := fi.Size() - 200000
		if start < 0 {
			start = 0
		}
		_, _ = f.Seek(start, 0)
		b, _ := io.ReadAll(f)
		f.Close()
		ms := regexp.MustCompile(`Started serving on (\d+)`).FindAllStringSubmatch(string(b), -1)
		if len(ms) > 0 {
			var n int
			fmt.Sscanf(ms[len(ms)-1][1], "%d", &n)
			return n
		}
	}
	return 0
}

func validNwid(id string) bool { return regexp.MustCompile(`^[0-9a-fA-F]{16}$`).MatchString(id) }

// ztInstall скачивает официальный установщик ZeroTier One и запускает его (Windows).
func (a *App) ztInstall(ctx context.Context) error {
	if runtime.GOOS != "windows" {
		return errors.New(a.tr("Установите ZeroTier One командой: curl -s https://install.zerotier.com | sudo bash", "Install ZeroTier One with: curl -s https://install.zerotier.com | sudo bash"))
	}
	a.setStage(a.tr("Скачивание ZeroTier One…", "Downloading ZeroTier One…"), 0, 0)
	dir, err := os.MkdirTemp("", "mobilix-zt")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	msi := filepath.Join(dir, "ZeroTierOne.msi")
	if err := a.fetchFile(ctx, "https://download.zerotier.com/dist/ZeroTier%20One.msi", msi, "", 0, nil); err != nil {
		return err
	}
	a.setStage(a.tr("Установка ZeroTier One (подтвердите запрос Windows)…", "Installing ZeroTier One (confirm the Windows prompt)…"), 0, 0)
	ps := "Start-Process msiexec -ArgumentList '/i','" + strings.ReplaceAll(msi, "'", "''") + "','/passive','/norestart' -Verb RunAs -Wait"
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	hideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(a.tr("Установка не выполнена: ", "Install failed: ") + strings.TrimSpace(string(out)))
	}
	a.lg.add("[Mobilix] ZeroTier One " + a.tr("установлен", "installed"))
	return nil
}

func (a *App) ztRoutes(mux *http.ServeMux) {
	a.ztcRoutes(mux)
	a.api(mux, "/api/zt/install", func(w http.ResponseWriter, r *http.Request) {
		a.enqueue("zt-install", "zt", a.ztInstall)
		jw(w, map[string]any{"queued": true})
	})
	a.api(mux, "/api/zt/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		jw(w, a.ztState(ctx))
	})
	a.api(mux, "/api/zt/join", func(w http.ResponseWriter, r *http.Request) {
		var b ZtNet
		if readBody(r, &b) != nil || !validNwid(strings.TrimSpace(b.ID)) {
			jerr(w, 400, a.tr("ID сети — 16 символов (0-9, a-f)", "Network ID is 16 hex characters"))
			return
		}
		b.ID = strings.ToLower(strings.TrimSpace(b.ID))
		a.saveSettings(func(s *Settings) {
			for i, n := range s.ZtNets {
				if n.ID == b.ID {
					s.ZtNets[i].Name = b.Name
					return
				}
			}
			s.ZtNets = append(s.ZtNets, b)
		})
		if err := ztCall(r.Context(), "POST", "/network/"+b.ID, map[string]any{}, nil); err != nil {
			jerr(w, 502, a.tr("ZeroTier One не запущен или не установлен", "ZeroTier One is not running or not installed"))
			return
		}
		a.saveSettings(func(s *Settings) { s.ZtOn = true })
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/zt/leave", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID     string
			Forget bool
		}
		if readBody(r, &b) != nil || !validNwid(b.ID) {
			jerr(w, 400, "bad id")
			return
		}
		_ = ztCall(r.Context(), "DELETE", "/network/"+strings.ToLower(b.ID), nil, nil)
		if b.Forget {
			a.saveSettings(func(s *Settings) {
				var keep []ZtNet
				for _, n := range s.ZtNets {
					if n.ID != strings.ToLower(b.ID) {
						keep = append(keep, n)
					}
				}
				s.ZtNets = keep
			})
		}
		jw(w, map[string]any{"ok": true})
	})
	// главный переключатель: включить = войти во все сохранённые сети, выключить = выйти из них
	a.api(mux, "/api/zt/toggle", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ On bool }
		_ = readBody(r, &b)
		if !ztInstalled() || ztToken() == "" {
			jerr(w, 409, a.tr("ZeroTier One не установлен", "ZeroTier One is not installed"))
			return
		}
		s := a.settings()
		for _, n := range s.ZtNets {
			if b.On {
				if err := ztCall(r.Context(), "POST", "/network/"+n.ID, map[string]any{}, nil); err != nil {
					jerr(w, 502, a.tr("ZeroTier One не отвечает. Запустите его.", "ZeroTier One is not responding. Start it."))
					return
				}
			} else {
				_ = ztCall(r.Context(), "DELETE", "/network/"+n.ID, nil, nil)
			}
		}
		a.saveSettings(func(s *Settings) { s.ZtOn = b.On })
		jw(w, map[string]any{"on": b.On})
	})
	a.api(mux, "/api/zt/share", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		st := a.ztState(ctx)
		ip := ""
		for _, n := range st.Nets {
			if len(n.IPs) > 0 {
				ip = n.IPs[0]
				break
			}
		}
		if ip == "" {
			jerr(w, 404, a.tr("Нет адреса в сети ZeroTier. Войдите в сеть и дождитесь разрешения от хозяина.", "No ZeroTier address yet. Join a network and wait for the owner to authorise you."))
			return
		}
		txt := "[ZeroTier] IP: " + ip
		if st.Port > 0 {
			txt += fmt.Sprintf(":%d", st.Port)
		}
		jw(w, map[string]any{"text": txt, "ip": ip, "port": st.Port})
	})
}

// ---- ZeroTier Central: создать и настроить свою сеть по токену с my.zerotier.com ----

func (a *App) ztCentral(ctx context.Context, method, path string, body, out any) error {
	tok := strings.TrimSpace(a.settings().ZtApi)
	if tok == "" {
		return errors.New(a.tr("Вставьте токен с my.zerotier.com", "Paste the token from my.zerotier.com"))
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.zerotier.com/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return errors.New(a.tr("ZeroTier Central недоступен: ", "ZeroTier Central is unreachable: ") + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return errors.New(a.tr("Токен не подходит. Создайте новый на my.zerotier.com → Account → API Access Tokens.", "The token was rejected. Create a new one at my.zerotier.com → Account → API Access Tokens."))
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ZeroTier Central: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type ztcNet struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Private bool   `json:"private"`
	Members int    `json:"members"`
	Online  int    `json:"online"`
	Subnet  string `json:"subnet"`
}

func (a *App) ztcRoutes(mux *http.ServeMux) {
	tctx := func(r *http.Request) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), 25*time.Second)
	}
	a.api(mux, "/api/zt/central/token", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Token string }
		_ = readBody(r, &b)
		b.Token = strings.TrimSpace(b.Token)
		ctx, cancel := tctx(r)
		defer cancel()
		if b.Token != "" {
			a.saveSettings(func(s *Settings) { s.ZtApi = b.Token })
			var me struct {
				DisplayName string `json:"displayName"`
			}
			if err := a.ztCentral(ctx, "GET", "/status", nil, &me); err != nil {
				a.saveSettings(func(s *Settings) { s.ZtApi = "" })
				jerr(w, 502, err.Error())
				return
			}
		} else {
			a.saveSettings(func(s *Settings) { s.ZtApi = "" })
		}
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/zt/central/networks", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := tctx(r)
		defer cancel()
		var raw []struct {
			ID     string `json:"id"`
			Config struct {
				Name    string `json:"name"`
				Private bool   `json:"private"`
				Routes  []struct {
					Target string `json:"target"`
				} `json:"routes"`
			} `json:"config"`
			Total  int `json:"totalMemberCount"`
			Online int `json:"onlineMemberCount"`
		}
		if err := a.ztCentral(ctx, "GET", "/network", nil, &raw); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		out := []ztcNet{}
		for _, n := range raw {
			x := ztcNet{ID: n.ID, Name: n.Config.Name, Private: n.Config.Private, Members: n.Total, Online: n.Online}
			if len(n.Config.Routes) > 0 {
				x.Subnet = n.Config.Routes[0].Target
			}
			out = append(out, x)
		}
		jw(w, map[string]any{"nets": out})
	})
	a.api(mux, "/api/zt/central/create", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name string }
		_ = readBody(r, &b)
		name := strings.TrimSpace(b.Name)
		if name == "" {
			name = "Mobilix"
		}
		ctx, cancel := tctx(r)
		defer cancel()
		oct := 144 + int(time.Now().UnixNano()%4) // 10.144–147.x.0/24
		third := int(time.Now().UnixNano()/7) % 250
		pre := fmt.Sprintf("10.%d.%d", oct, third)
		body := map[string]any{"config": map[string]any{
			"name": name, "private": true,
			"v4AssignMode":      map[string]any{"zt": true},
			"routes":            []map[string]any{{"target": pre + ".0/24"}},
			"ipAssignmentPools": []map[string]any{{"ipRangeStart": pre + ".1", "ipRangeEnd": pre + ".254"}},
		}}
		var res struct {
			ID string `json:"id"`
		}
		if err := a.ztCentral(ctx, "POST", "/network", body, &res); err != nil || res.ID == "" {
			if err == nil {
				err = errors.New("empty id")
			}
			jerr(w, 502, err.Error())
			return
		}
		id := strings.ToLower(res.ID)
		a.saveSettings(func(s *Settings) { s.ZtNets = append(s.ZtNets, ZtNet{ID: id, Name: name}) })
		// сразу входим в сеть и разрешаем себя
		if ztCall(ctx, "POST", "/network/"+id, map[string]any{}, nil) == nil {
			var st struct {
				Address string `json:"address"`
			}
			if ztCall(ctx, "GET", "/status", nil, &st) == nil && st.Address != "" {
				_ = a.ztCentral(ctx, "POST", "/network/"+id+"/member/"+st.Address, map[string]any{"config": map[string]any{"authorized": true}}, nil)
			}
			a.saveSettings(func(s *Settings) { s.ZtOn = true })
		}
		jw(w, map[string]any{"id": id})
	})
	a.api(mux, "/api/zt/central/update", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID      string
			Name    string
			Private *bool
		}
		if readBody(r, &b) != nil || !validNwid(b.ID) {
			jerr(w, 400, "bad id")
			return
		}
		cfg := map[string]any{}
		if n := strings.TrimSpace(b.Name); n != "" {
			cfg["name"] = n
		}
		if b.Private != nil {
			cfg["private"] = *b.Private
		}
		ctx, cancel := tctx(r)
		defer cancel()
		if err := a.ztCentral(ctx, "POST", "/network/"+strings.ToLower(b.ID), map[string]any{"config": cfg}, nil); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		if n := strings.TrimSpace(b.Name); n != "" {
			a.saveSettings(func(s *Settings) {
				for i := range s.ZtNets {
					if s.ZtNets[i].ID == strings.ToLower(b.ID) {
						s.ZtNets[i].Name = n
					}
				}
			})
		}
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/zt/central/members", func(w http.ResponseWriter, r *http.Request) {
		id := strings.ToLower(r.URL.Query().Get("id"))
		if !validNwid(id) {
			jerr(w, 400, "bad id")
			return
		}
		ctx, cancel := tctx(r)
		defer cancel()
		var raw []struct {
			NodeID string `json:"nodeId"`
			Name   string `json:"name"`
			Online bool   `json:"online"`
			Config struct {
				Authorized bool     `json:"authorized"`
				IPs        []string `json:"ipAssignments"`
			} `json:"config"`
		}
		if err := a.ztCentral(ctx, "GET", "/network/"+id+"/member", nil, &raw); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		type mem struct {
			Node       string   `json:"node"`
			Name       string   `json:"name"`
			Online     bool     `json:"online"`
			Authorized bool     `json:"authorized"`
			IPs        []string `json:"ips"`
		}
		out := []mem{}
		for _, m := range raw {
			out = append(out, mem{m.NodeID, m.Name, m.Online, m.Config.Authorized, m.Config.IPs})
		}
		jw(w, map[string]any{"members": out})
	})
	a.api(mux, "/api/zt/central/auth", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			ID, Node string
			Ok       bool
		}
		if readBody(r, &b) != nil || !validNwid(b.ID) || !regexp.MustCompile(`^[0-9a-fA-F]{10}$`).MatchString(b.Node) {
			jerr(w, 400, "bad id")
			return
		}
		ctx, cancel := tctx(r)
		defer cancel()
		if err := a.ztCentral(ctx, "POST", "/network/"+strings.ToLower(b.ID)+"/member/"+strings.ToLower(b.Node), map[string]any{"config": map[string]any{"authorized": b.Ok}}, nil); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		jw(w, map[string]any{"ok": true})
	})
	a.api(mux, "/api/zt/central/delete", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ ID string }
		if readBody(r, &b) != nil || !validNwid(b.ID) {
			jerr(w, 400, "bad id")
			return
		}
		ctx, cancel := tctx(r)
		defer cancel()
		if err := a.ztCentral(ctx, "DELETE", "/network/"+strings.ToLower(b.ID), nil, nil); err != nil {
			jerr(w, 502, err.Error())
			return
		}
		_ = ztCall(ctx, "DELETE", "/network/"+strings.ToLower(b.ID), nil, nil)
		a.saveSettings(func(s *Settings) {
			var keep []ZtNet
			for _, n := range s.ZtNets {
				if n.ID != strings.ToLower(b.ID) {
					keep = append(keep, n)
				}
			}
			s.ZtNets = keep
		})
		jw(w, map[string]any{"ok": true})
	})
}

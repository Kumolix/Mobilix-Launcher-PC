package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const elyAuth = "https://authserver.ely.by/auth"

// Account — вход игрока. Тип "ely" — аккаунт Ely.by; пустой — автономный режим с ником из настроек.
// Токен хранится отдельно от settings.json и в интерфейс не отдаётся.
type Account struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	Token  string `json:"token"`
	Client string `json:"client"`
}

func (a *App) accountFile() string { return a.path("account.json") }

func (a *App) account() Account {
	var ac Account
	if b, err := os.ReadFile(a.accountFile()); err == nil {
		_ = json.Unmarshal(b, &ac)
	}
	return ac
}

func (a *App) saveAccount(ac Account) {
	if ac.Type == "" {
		_ = os.Remove(a.accountFile())
		return
	}
	b, _ := json.Marshal(ac)
	_ = os.WriteFile(a.accountFile(), b, 0o600)
}

func (ac Account) public() map[string]any {
	return map[string]any{"type": ac.Type, "name": ac.Name}
}

func (a *App) clientToken(ac *Account) string {
	if ac.Client == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		ac.Client = hex.EncodeToString(b)
	}
	return ac.Client
}

func postJSON(ctx context.Context, u string, body any) (int, []byte, error) {
	b, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mobilix-PC/"+appVersion)
	resp, err := httpc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, out, nil
}

// elyLogin: вход по нику/почте и паролю (двухфакторный код дописывается к паролю через «:»).
func (a *App) elyLogin(ctx context.Context, user, pass string) error {
	ac := a.account()
	code, text, err := postJSON(ctx, elyAuth+"/authenticate", map[string]any{
		"username": user, "password": pass, "clientToken": a.clientToken(&ac), "requestUser": true})
	if err != nil {
		return errors.New(a.tr("Нет связи с Ely.by", "Cannot reach Ely.by"))
	}
	if code < 200 || code > 299 {
		var e struct {
			Message string `json:"errorMessage"`
		}
		_ = json.Unmarshal(text, &e)
		switch {
		case strings.Contains(strings.ToLower(e.Message), "two factor"):
			return errors.New("2FA")
		case code == 401 || code == 403:
			return errors.New(a.tr("Неверный логин или пароль", "Wrong login or password"))
		}
		return errors.New(strings.TrimSpace("Ely.by: " + e.Message))
	}
	var j struct {
		Token   string `json:"accessToken"`
		Profile struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"selectedProfile"`
	}
	if json.Unmarshal(text, &j) != nil || j.Token == "" || j.Profile.ID == "" {
		return errors.New(a.tr("Ely.by вернул неожиданный ответ", "Ely.by returned an unexpected reply"))
	}
	a.saveAccount(Account{Type: "ely", Name: j.Profile.Name, UUID: j.Profile.ID, Token: j.Token, Client: ac.Client})
	a.saveSettings(func(s *Settings) { s.SkinSrc = "ely"; s.SkinNick = "" })
	return nil
}

// elyEnsure проверяет токен и при необходимости обновляет; без сети запускаем с текущим.
func (a *App) elyEnsure(ctx context.Context) bool {
	ac := a.account()
	if ac.Type != "ely" {
		return true
	}
	if code, _, err := postJSON(ctx, elyAuth+"/validate", map[string]any{"accessToken": ac.Token}); err != nil || (code >= 200 && code < 300) {
		return true
	}
	code, text, _ := postJSON(ctx, elyAuth+"/refresh", map[string]any{"accessToken": ac.Token, "clientToken": ac.Client})
	if code < 200 || code > 299 {
		return false
	}
	var j struct {
		Token string `json:"accessToken"`
	}
	if json.Unmarshal(text, &j) != nil || j.Token == "" {
		return false
	}
	ac.Token = j.Token
	a.saveAccount(ac)
	return true
}

// ensureAuthlib скачивает authlib-injector (AGPL-3.0): он подменяет серверы авторизации и скинов на Ely.by.
func (a *App) ensureAuthlib(ctx context.Context) (string, error) {
	f := a.path("cache", "authlib-injector.jar")
	if st, err := os.Stat(f); err == nil && st.Size() > 10000 {
		return f, nil
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := getJSON(ctx, "https://api.github.com/repos/yushijinhun/authlib-injector/releases/latest", &rel); err != nil {
		return "", err
	}
	for _, x := range rel.Assets {
		if strings.HasSuffix(x.Name, ".jar") {
			if err := a.fetchFile(ctx, x.URL, f, "", 0, nil); err != nil {
				return "", err
			}
			return f, nil
		}
	}
	return "", errors.New("authlib-injector: no jar in release")
}

// playerName — ник для игры: ник аккаунта Ely.by или ник из настроек.
func (a *App) playerName(s Settings) (name, uuid, token, utype string) {
	if ac := a.account(); ac.Type == "ely" {
		return ac.Name, ac.UUID, ac.Token, "mojang"
	}
	return s.Nick, offlineUUID(s.Nick), "0", "legacy"
}

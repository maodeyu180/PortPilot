package control

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
)

// The caller holds loginMu.
func (a *App) allowAuthAttempt(w http.ResponseWriter, r *http.Request) (string, bool) {
	now := time.Now()
	key, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		key = r.RemoteAddr
	}
	if len(a.loginFailures) > 1024 {
		a.loginFailures = map[string][]time.Time{}
	}
	attempts := []time.Time{}
	for _, attempt := range a.loginFailures[key] {
		if now.Sub(attempt) < 5*time.Minute {
			attempts = append(attempts, attempt)
		}
	}
	a.loginFailures[key] = attempts
	if len(attempts) >= 10 {
		w.Header().Set("Retry-After", "300")
		fail(w, 429, "尝试次数过多，请 5 分钟后重试")
		return key, false
	}
	return key, true
}

func (a *App) authenticated(w http.ResponseWriter, r *http.Request) {
	expires := strconv.FormatInt(time.Now().Add(12*time.Hour).Unix(), 10)
	http.SetCookie(w, &http.Cookie{Name: "portpilot_session", Value: expires + "." + a.signature(expires), Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	respond(w, 200, map[string]bool{"authenticated": true, "passwordRequired": true, "setupRequired": false})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "请输入管理密码")
		return
	}
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.auth.setupRequired() {
		fail(w, 409, "请先完成首次设置")
		return
	}
	key, ok := a.allowAuthAttempt(w, r)
	if !ok {
		return
	}
	if !a.auth.verify(body.Password) {
		a.loginFailures[key] = append(a.loginFailures[key], time.Now())
		fail(w, 401, "管理密码不正确")
		return
	}
	delete(a.loginFailures, key)
	a.authenticated(w, r)
}

func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirmPassword"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "请填写管理密码与确认密码")
		return
	}
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if !a.auth.setupRequired() {
		fail(w, 409, errAlreadyInitialized.Error())
		return
	}
	if err := validateAdminPassword(body.Password); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if body.Password != body.ConfirmPassword {
		fail(w, 400, "两次输入的密码不一致")
		return
	}
	err := a.auth.initialize(body.Password)
	if errors.Is(err, errAlreadyInitialized) {
		fail(w, 409, err.Error())
		return
	}
	if err != nil {
		fail(w, 500, "无法保存管理密码，请检查数据目录是否可写后重试")
		return
	}
	a.authenticated(w, r)
}

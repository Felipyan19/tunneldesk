package app

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Felipyan19/tunneldesk/internal/openvpn"
	"github.com/Felipyan19/tunneldesk/internal/vault"
)

//go:embed web/*
var webFiles embed.FS

const sessionCookie = "tunneldesk_session"

func (a *Application) ServeUI() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	token, err := openvpn.NewSessionID()
	if err != nil {
		listener.Close()
		return err
	}
	origin := "http://" + listener.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		if !sameSecret(r.URL.Query().Get("token"), token) {
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/web/", http.StatusSeeOther)
	})

	api := http.NewServeMux()
	api.HandleFunc("GET /api/profiles", a.apiProfiles)
	api.HandleFunc("POST /api/profiles", a.apiAddProfile)
	api.HandleFunc("POST /api/profiles/{name}/credentials", a.apiCredentials)
	api.HandleFunc("POST /api/profiles/{name}/{action}", a.apiAction)

	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	api.HandleFunc("POST /api/quit", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		go shutdown(server)
	})
	mux.Handle("/api/", localSession(token, origin, api))
	mux.Handle("GET /", http.FileServer(http.FS(webFiles)))
	server.Handler = securityHeaders(mux)

	url := origin + "/session?token=" + token
	go func() { _ = openBrowser(url) }()
	fmt.Fprintf(a.Out, "TunnelDesk visual interface: %s\n", origin)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *Application) apiProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles, err := a.Profiles.List()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	type item struct {
		Name        string `json:"name"`
		Status      string `json:"status"`
		AutoConnect bool   `json:"autoConnect"`
	}
	result := make([]item, 0, len(profiles))
	for _, p := range profiles {
		state, _ := a.VPN.Status(p.Name)
		result = append(result, item{Name: p.Name, Status: state.Status, AutoConnect: p.AutoConnect})
	}
	writeJSON(w, result)
}

func (a *Application) apiAddProfile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name       string `json:"name"`
		ConfigPath string `json:"configPath"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	p, err := a.Profiles.Add(request.Name, request.ConfigPath)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	writeJSON(w, p)
}

func (a *Application) apiCredentials(w http.ResponseWriter, r *http.Request) {
	var credentials vault.Credentials
	if err := decodeJSON(r, &credentials); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	p, err := a.Profiles.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	if err := a.Vault.Put(p.Name, credentials); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"saved": true})
}

func (a *Application) apiAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	switch action {
	case "connect":
		state, err := a.startRunner(name)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, state)
	case "disconnect":
		if err := a.VPN.Disconnect(name); err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		writeError(w, fmt.Errorf("unknown action %q", action), http.StatusNotFound)
	}
}

func decodeJSON(r *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func localSession(token, origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != strings.TrimPrefix(origin, "http://") {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if requestOrigin := r.Header.Get("Origin"); requestOrigin != "" && requestOrigin != origin {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !sameSecret(cookie.Value, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameSecret(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func openBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}

func shutdown(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

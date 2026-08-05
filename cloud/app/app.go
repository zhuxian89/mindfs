package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mindfs-cloud/internal/binding"
	"mindfs-cloud/internal/config"
	"mindfs-cloud/internal/connector"
	"mindfs-cloud/internal/gateway"
	"mindfs-cloud/internal/identity"
	"mindfs-cloud/internal/ops"
	"mindfs-cloud/internal/store"
)

type App struct {
	config    config.Config
	store     store.Store
	binding   binding.BindingService
	identity  *identity.Service
	mail      identity.MailSender
	registry  *connector.Registry
	connector *connector.Handler
	gateway   *gateway.Handler
	metrics   *ops.Metrics
	handler   http.Handler
	assets    *os.Root
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func New(cfg config.Config) (*App, error) {
	return newApp(cfg, nil, nil)
}

func newApp(cfg config.Config, mail identity.MailSender, passwords *identity.PasswordHasher) (*App, error) {
	st, err := store.OpenSQLite(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if _, _, err := st.ClaimOwnerlessNodes(context.Background(), cfg.BootstrapEmail, time.Now().UTC()); err != nil {
		_ = st.Close()
		return nil, err
	}
	if err := st.DeleteExpired(context.Background(), time.Now().UTC()); err != nil {
		_ = st.Close()
		return nil, err
	}
	identityKey, err := identity.LoadOrCreateKey(cfg.DataDir)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	if mail == nil {
		mail, err = identity.NewSMTPSender(cfg.SMTP)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
	}
	if passwords == nil {
		passwords = identity.NewPasswordHasher()
	}
	identityService, err := identity.NewService(st, passwords, mail, identityKey)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	tokens := binding.NewDeviceTokenService(cfg.TokenKey, st)
	registry := connector.NewSessionRegistry()
	assets, err := os.OpenRoot(filepath.Join(cfg.AssetsDir, "assets"))
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	app := &App{
		config:    cfg,
		store:     st,
		binding:   binding.NewService(st, tokens, cfg.BindTTL, publicWebSocketURL(cfg), cfg.PublicURL.String()),
		identity:  identityService,
		mail:      mail,
		registry:  registry,
		connector: connector.NewHandler(tokens, registry, cfg.StreamOpenTimeout),
		gateway:   gateway.NewHandler(st, registry, cfg.StreamOpenTimeout, cfg.HeaderTimeout, cfg.PublicURL.Scheme),
		metrics:   ops.NewMetrics(),
		assets:    assets,
	}
	app.handler = app.routes()
	cleanupCtx, cancel := context.WithCancel(context.Background())
	app.cancel = cancel
	app.wg.Add(1)
	go app.runCleanup(cleanupCtx)
	return app, nil
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.cancel()
		a.wg.Wait()
		_ = a.registry.Close()
		_ = a.assets.Close()
		a.closeErr = a.store.Close()
	})
	return a.closeErr
}

func (a *App) runCleanup(ctx context.Context) {
	defer a.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := a.store.DeleteExpired(ctx, now.UTC()); err != nil {
				log.Printf("cleanup failed: %v", err)
			}
		}
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /readyz", a.handleReady)
	mux.HandleFunc("GET /metrics", a.handleMetrics)
	mux.HandleFunc("GET /mindfs-assets/", a.handleAsset)
	mux.HandleFunc("GET /{$}", a.handleBrowserRoot)
	mux.HandleFunc("GET /login", a.handleBrowserLogin)
	mux.HandleFunc("GET /nodes", a.handleBrowserNodes)
	mux.HandleFunc("POST /api/auth/register/request-code", a.handleRegistrationCodeRequest)
	mux.HandleFunc("POST /api/auth/register", a.handleRegistration)
	mux.HandleFunc("POST /api/auth/login", a.handlePasswordLogin)
	mux.HandleFunc("POST /api/auth/password/request-code", a.handlePasswordResetCodeRequest)
	mux.HandleFunc("POST /api/auth/password/reset", a.handlePasswordReset)
	mux.HandleFunc("POST /api/auth/password/change", a.handlePasswordChange)
	mux.HandleFunc("GET /api/auth/me", a.handleBrowserAuthStatus)
	mux.HandleFunc("POST /api/auth/logout", a.handleBrowserLogout)
	mux.HandleFunc("GET /api/nodes", a.handleRelayNodesList)
	mux.HandleFunc("PATCH /api/nodes/{id}", a.handleRelayNodeRename)
	mux.HandleFunc("DELETE /api/nodes/{id}", a.handleRelayNodeDelete)
	mux.HandleFunc("GET /bind", a.handleBindPage)
	mux.HandleFunc("GET /api/bind/poll", a.handleBindPoll)
	mux.HandleFunc("GET /api/bind/status", a.handleBindStatus)
	mux.HandleFunc("POST /api/bind/confirm", a.handleBindConfirm)
	mux.HandleFunc("GET /ws/connector", a.handleConnector)
	mux.HandleFunc("/n/", a.handleGateway)
	return requestIDMiddleware(requestLogMiddleware(mux, a.metrics.ObserveHTTP))
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	respondJSON(w, status, map[string]string{
		"error":      code,
		"message":    message,
		"request_id": requestIDFromContext(r.Context()),
	})
}

func publicWebSocketURL(cfg config.Config) string {
	base := *cfg.PublicURL
	if base.Scheme == "https" {
		base.Scheme = "wss"
	} else {
		base.Scheme = "ws"
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/ws/connector"
	return base.String()
}

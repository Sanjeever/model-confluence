package app

import (
	"log/slog"
	"net/http"

	"github.com/Sanjeever/model-confluence/internal/admin"
	"github.com/Sanjeever/model-confluence/internal/config"
	"github.com/Sanjeever/model-confluence/internal/gateway"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/store"
	"github.com/Sanjeever/model-confluence/internal/webui"
)

func New(cfg config.Config, database *store.Store) http.Handler {
	clientIP, err := httpx.NewClientIPResolver(cfg.TrustedProxyCIDRs)
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if err := database.Ping(); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	gatewayHandler := gateway.NewHandler(cfg, database, clientIP)
	admin.NewHandler(database, clientIP, gatewayHandler).Register(mux)
	gatewayHandler.Register(mux)
	mux.Handle("/", webui.Handler())

	return securityHeaders(requestLog(mux))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("http request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

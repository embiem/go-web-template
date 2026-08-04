package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/embiem/go-web-template/db"
	"github.com/embiem/go-web-template/handler"
	"github.com/embiem/go-web-template/util"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/joho/godotenv"
)

func main() {
	initialize()
	defer teardown()

	// Setup Chi router
	router := chi.NewRouter()

	// Middleware stack
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Heartbeat("/healthz"))
	router.Use(util.RequestLogger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Compress(5))
	router.Use(middleware.Timeout(30 * time.Second))

	router.Get("/", handler.Make(handler.GetIndexPage))

	// Auth Routes
	router.Get("/signup", handler.Make(handler.GetSignupPage))
	router.Post("/signup", handler.Make(handler.PostSignup))

	router.Get("/login", handler.Make(handler.GetLoginPage))
	router.Post("/login", handler.Make(handler.PostLogin))

	router.Post("/logout", handler.Make(handler.PostLogout))

	// Static files
	filesDir := http.Dir("public")
	fileServer(router, "/public", filesDir)

	// Configure the HTTP server with sane timeouts
	addr := ":" + getenv("PORT", "3000")
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler.SessionManager.LoadAndSave(router),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start listening in the background
	go func() {
		slog.Info("Listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for an interrupt, then drain HTTP before closing the DB pool
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}

type cacheOnSuccessWriter struct {
	http.ResponseWriter
	cacheControl string
	wroteHeader  bool
}

func (w *cacheOnSuccessWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		if status < http.StatusBadRequest {
			w.Header().Set("Cache-Control", w.cacheControl)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cacheOnSuccessWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func fileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit any URL parameters.")
	}

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", http.StatusMovedPermanently).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		cw := &cacheOnSuccessWriter{ResponseWriter: w, cacheControl: "public, max-age=3600"}
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(cw, r)
	})
}

func initialize() {
	// Load env vars
	if err := godotenv.Load(); err != nil {
		slog.Warn("couldn't load env vars", "err", err)
	}

	appEnv := getenv("APP_ENV", "development")

	// Configure the default structured logger based on the environment
	var handlerImpl slog.Handler
	if appEnv == "production" {
		handlerImpl = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		handlerImpl = slog.NewTextHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(handlerImpl))

	// Setup DB
	if err := db.Init(); err != nil {
		slog.Error("couldn't init db", "err", err)
		os.Exit(1)
	}

	handler.InitSession(appEnv == "production")
}

func teardown() {
	slog.Info("Teardown started...")
	db.Teardown()
	slog.Info("Teardown finished.")
}

// getenv returns the value of the environment variable named by key, or
// fallback when the variable is unset or empty.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/embiem/go-web-template/db"
	"github.com/embiem/go-web-template/handler"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/joho/godotenv"
)

func main() {
	initialize()
	defer teardown()

	// Ensure we're calling teardown on interrupt
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		teardown()
		os.Exit(0)
	}()

	// Setup Chi router
	router := chi.NewRouter()

	// Middleware stack
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	// Static files
	router.Handle("/public/", http.FileServer(http.Dir("public")))

	// Health check
	router.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})

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

	// Start listening
	slog.Info("Listening on :3000")
	http.ListenAndServe(":3000", handler.SessionManager.LoadAndSave(router))
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
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(w, r)
	})
}

func initialize() {
	// Load env vars
	if err := godotenv.Load(); err != nil {
		slog.Warn("couldn't load env vars", "err", err)
	}

	// Setup DB
	if err := db.Init(); err != nil {
		log.Fatalf("couldn't init db: %v", err)
	}

	handler.InitSession()
}

func teardown() {
	slog.Info("Teardown started...")
	db.Teardown()
	slog.Info("Teardown finished.")
}

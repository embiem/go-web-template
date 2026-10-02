package handler

import "net/http"

// CacheControl adds a Cache-Control header to successful (status < 400)
// responses only, so error/redirect paths stay uncached. Used for the public
// pages ("public, max-age=300"); static file serving does its own.
func CacheControl(value string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&cacheOnSuccessWriter{ResponseWriter: w, cacheControl: value}, r)
		})
	}
}

// cacheOnSuccessWriter only attaches the header once a success status is
// written; handlers that render 404s or set their own cache headers run
// before it.
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

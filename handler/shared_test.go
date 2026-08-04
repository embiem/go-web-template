package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMake(t *testing.T) {
	t.Run("nil error leaves status 200", func(t *testing.T) {
		h := Make(func(w http.ResponseWriter, r *http.Request) error {
			w.Write([]byte("ok"))
			return nil
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("returned error yields 500 with body", func(t *testing.T) {
		h := Make(func(w http.ResponseWriter, r *http.Request) error {
			return errors.New("boom")
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		if rec.Body.Len() == 0 {
			t.Error("expected non-empty body on error, got empty")
		}
	})
}

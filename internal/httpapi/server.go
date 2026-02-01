package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"tor_project/internal/db"
)

type Server struct {
	store      *db.Store
	storageDir string
	botToken   string
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func New(store *db.Store, storageDir string, botToken string) *Server {
	return &Server{
		store:      store,
		storageDir: storageDir,
		botToken:   botToken,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/library", s.handleLibrary)
	mux.HandleFunc("/api/files/", s.handleFile)
	mux.HandleFunc("/api/progress", s.handleProgress)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(rec, r)
		log.Printf("http %s %s -> %d ua=%s", r.Method, r.URL.Path, rec.status, r.UserAgent())
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	s.withUser(w, r, func(ctx context.Context, user TelegramUser) {
		log.Printf("library: request user_id=%d ua=%s", user.ID, r.UserAgent())
		items, err := s.store.ListLibrary(ctx, user.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	s.withUser(w, r, func(ctx context.Context, user TelegramUser) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/files/")
		if idStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file id пустой"})
			return
		}
		fileID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file id некорректен"})
			return
		}

		file, err := s.store.GetFileForUser(ctx, user.ID, fileID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
			return
		}

		fullPath := filepath.Join(s.storageDir, file.Path)
		setContentType(w, file.Format)
		http.ServeFile(w, r, fullPath)
	})
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	s.withUser(w, r, func(ctx context.Context, user TelegramUser) {
		var body struct {
			FileID   int64  `json:"file_id"`
			Location string `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if body.FileID == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_id пустой"})
			return
		}
		if err := s.store.UpdateProgress(ctx, user.ID, body.FileID, body.Location); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func (s *Server) withUser(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, user TelegramUser)) {
	initData := extractInitData(r)
	if initData == "" {
		log.Printf("auth: initData missing remote=%s ua=%s", r.RemoteAddr, r.UserAgent())
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "initData required"})
		return
	}

	user, err := ValidateInitData(initData, s.botToken)
	if err != nil {
		log.Printf("auth: initData invalid len=%d remote=%s ua=%s err=%v", len(initData), r.RemoteAddr, r.UserAgent(), err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid initData"})
		return
	}

	if err := s.store.EnsureUser(r.Context(), user.ID, user.Username); err != nil {
		log.Printf("EnsureUser error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db error"})
		return
	}

	log.Printf("auth: ok user_id=%d username=%s", user.ID, user.Username)
	fn(r.Context(), user)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func setContentType(w http.ResponseWriter, format string) {
	format = strings.ToLower(strings.TrimSpace(format))
	switch {
	case strings.Contains(format, "epub"):
		w.Header().Set("Content-Type", "application/epub+zip")
	case strings.Contains(format, "fb2") && strings.Contains(format, "zip"):
		w.Header().Set("Content-Type", "application/zip")
	case strings.Contains(format, "fb2"):
		w.Header().Set("Content-Type", "application/xml")
	case strings.Contains(format, "pdf"):
		w.Header().Set("Content-Type", "application/pdf")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
}

func extractInitData(r *http.Request) string {
	// Headers first (preferred channel for security-critical data).
	if v := r.Header.Get("X-Telegram-InitData"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Telegram-Web-App-Data"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Telegram-WebApp-Data"); v != "" {
		return v
	}

	if auth := r.Header.Get("Authorization"); auth != "" {
		low := strings.ToLower(auth)
		if strings.HasPrefix(low, "tma ") {
			return strings.TrimSpace(auth[4:])
		}
	}

	// URL fallbacks (useful for debugging in a normal browser).
	if v := r.URL.Query().Get("initData"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("tgWebAppData"); v != "" {
		return v
	}
	return ""
}

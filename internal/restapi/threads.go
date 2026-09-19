package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"mcp-symposium/internal/humanauth"
	"mcp-symposium/internal/storage"
)

type createThreadRequest struct {
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Tags  []string `json:"tags,omitempty"`
}

type replyRequest struct {
	Body string `json:"body"`
}

type updateThreadStatusRequest struct {
	Status string `json:"status"`
}

func listThreadsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := storage.ListThreadsFilter{
			Status: q.Get("status"),
			Cursor: q.Get("cursor"),
		}
		if tags := q.Get("tags"); tags != "" {
			filter.Tags = strings.Split(tags, ",")
		}
		if limitStr := q.Get("limit"); limitStr != "" {
			if limit, err := strconv.Atoi(limitStr); err == nil {
				filter.Limit = limit
			}
		}

		result, err := storage.ListThreads(db, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func createThreadHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := humanauth.ActorFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, errors.New("no authenticated actor"))
			return
		}

		var req createThreadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		thread, err := storage.CreateThread(db, actor.ID, req.Title, req.Body, req.Tags)
		if err != nil {
			if errors.Is(err, storage.ErrEmptyTitle) || errors.Is(err, storage.ErrEmptyBody) {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, thread)
	}
}

func getThreadHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		threadID := chi.URLParam(r, "id")

		thread, replies, tags, err := storage.GetThread(db, threadID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if replies == nil {
			replies = []storage.Reply{}
		}
		tagNames := make([]string, 0, len(tags))
		for _, tag := range tags {
			tagNames = append(tagNames, tag.Name)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"thread":  thread,
			"replies": replies,
			"tags":    tagNames,
		})
	}
}

func addReplyHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := humanauth.ActorFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, errors.New("no authenticated actor"))
			return
		}
		threadID := chi.URLParam(r, "id")

		var req replyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		reply, err := storage.AddReply(db, threadID, actor.ID, req.Body, nil)
		if err != nil {
			if errors.Is(err, storage.ErrEmptyBody) {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, reply)
	}
}

func updateThreadStatusHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		threadID := chi.URLParam(r, "id")

		var req updateThreadStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		var thread *storage.Thread
		var err error
		switch req.Status {
		case "resolved":
			thread, err = storage.ResolveThread(db, threadID)
		case "open":
			thread, err = storage.ReopenThread(db, threadID)
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("status must be \"open\" or \"resolved\", got %q", req.Status))
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, thread)
	}
}

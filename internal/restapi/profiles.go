package restapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"mcp-symposium/internal/humanauth"
	"mcp-symposium/internal/storage"
)

type profileRequest struct {
	Name     string   `json:"name"`
	Nickname string   `json:"nickname"`
	Bio      string   `json:"bio"`
	Tags     []string `json:"tags"`
}

type profileResponse struct {
	ActorID     string   `json:"actor_id"`
	DisplayName string   `json:"display_name"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name,omitempty"`
	Nickname    string   `json:"nickname,omitempty"`
	Bio         string   `json:"bio,omitempty"`
	Tags        []string `json:"tags"`
}

func toProfileResponse(view *storage.ProfileView) profileResponse {
	resp := profileResponse{
		ActorID:     view.Actor.ID,
		DisplayName: view.Actor.DisplayName,
		Kind:        string(view.Actor.Kind),
		Tags:        []string{},
	}
	if view.Profile != nil {
		resp.Name = view.Profile.Name
		resp.Nickname = view.Profile.Nickname
		resp.Bio = view.Profile.Bio
	}
	for _, tg := range view.Tags {
		resp.Tags = append(resp.Tags, tg.Name)
	}
	return resp
}

func getProfileHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actorID := chi.URLParam(r, "actorID")
		view, err := storage.GetProfileView(db, actorID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, toProfileResponse(view))
	}
}

func updateOwnProfileHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := humanauth.ActorFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, errors.New("no authenticated actor"))
			return
		}
		updateProfile(w, r, db, actor.ID)
	}
}

func updateProfileByIDHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		updateProfile(w, r, db, chi.URLParam(r, "actorID"))
	}
}

func updateProfile(w http.ResponseWriter, r *http.Request, db *gorm.DB, actorID string) {
	var req profileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if _, err := storage.UpsertActorProfile(db, actorID, req.Name, req.Nickname, req.Bio, req.Tags); err != nil {
		if errors.Is(err, storage.ErrEmptyName) || errors.Is(err, storage.ErrEmptyNickname) ||
			errors.Is(err, storage.ErrInvalidNickname) || errors.Is(err, storage.ErrNicknameTaken) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	view, err := storage.GetProfileView(db, actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, toProfileResponse(view))
}

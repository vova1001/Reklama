package httpapi

import (
	"net/http"

	"ohvat/backend/internal/domain"
)

// GET /api/me — кто вошёл: общие данные, режим, способы входа, какие профили заполнены, прогресс.
func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	o, err := s.profiles.Overview(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMeDTO(o))
}

// PATCH /api/me/mode — переключить режим «Бизнес / Автор».
func (s *Server) patchMode(w http.ResponseWriter, r *http.Request) {
	var in modeIn
	if !decode(w, r, &in) {
		return
	}
	o, err := s.profiles.SwitchMode(r.Context(), currentUser(r), in.Mode)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMeDTO(o))
}

// PUT /api/me/profile — общие данные: как вас называть и город.
func (s *Server) putBasics(w http.ResponseWriter, r *http.Request) {
	var in basicsIn
	if !decode(w, r, &in) {
		return
	}
	u, err := s.profiles.SaveBasics(r.Context(), currentUser(r), domain.Basics{Name: in.Name, City: in.City})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

// GET /api/me/profile/business — профиль бизнеса (profile: null — ещё не заполнен).
func (s *Server) getBusiness(w http.ResponseWriter, r *http.Request) {
	p, err := s.profiles.Business(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, businessOut{Profile: toBusinessDTO(p)})
}

// PUT /api/me/profile/business — сохранить профиль бизнеса.
func (s *Server) putBusiness(w http.ResponseWriter, r *http.Request) {
	var in businessIn
	if !decode(w, r, &in) {
		return
	}
	p, err := s.profiles.SaveBusiness(r.Context(), currentUser(r), domain.BusinessProfile{Name: in.Name, City: in.City, Spheres: in.Spheres})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, businessOut{Profile: toBusinessDTO(&p)})
}

// GET /api/me/profile/creator — профиль автора (profile: null — ещё не заполнен).
func (s *Server) getCreator(w http.ResponseWriter, r *http.Request) {
	p, err := s.profiles.Creator(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, creatorOut{Profile: toCreatorDTO(p)})
}

// PUT /api/me/profile/creator — сохранить профиль автора.
func (s *Server) putCreator(w http.ResponseWriter, r *http.Request) {
	var in creatorIn
	if !decode(w, r, &in) {
		return
	}
	p, err := s.profiles.SaveCreator(r.Context(), currentUser(r), domain.CreatorProfile{
		Name: in.Name, City: in.City, Spheres: in.Spheres, Channels: in.Channels,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, creatorOut{Profile: toCreatorDTO(&p)})
}

// POST /api/me/logout-all — выйти на всех устройствах (включая это).
func (s *Server) postLogoutAll(w http.ResponseWriter, r *http.Request) {
	n, err := s.auth.LogoutEverywhere(r.Context(), currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.clearCookie(w, sessionCookie, "/")
	writeJSON(w, http.StatusOK, logoutAllDTO{Sessions: n})
}

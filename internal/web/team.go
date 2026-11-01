package web

import (
	"net/http"
	"net/url"

	"github.com/GeneJie199/ai-dev-cycle-manager/internal/app"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/team"
)

func (s *Server) registerTeamRoutes() {
	s.mux.HandleFunc("GET /api/team/module-links", s.handleModuleLinks)
	s.mux.HandleFunc("GET /api/team/about", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, team.BuildAbout()) })
	s.mux.HandleFunc("GET /api/team/license", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(team.LicenseText))
	})
	// Lazily open the sidecar: v1-only sessions do not create or migrate team state.
	handle := func(pattern string, h http.HandlerFunc) {
		s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if _, err := s.app.TeamStore(); err != nil {
				s.fail(w, err)
				return
			}
			h(w, r)
		})
	}
	handle("GET /api/team/missions", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.app.Team.List(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	handle("POST /api/team/missions", func(w http.ResponseWriter, r *http.Request) {
		if !teamOrigin(w, r) {
			return
		}
		var m team.Mission
		if !decodeBody(w, r, &m) {
			return
		}
		v, err := s.app.CreateMission(r.Context(), m)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 201, v)
	})
	handle("GET /api/team/missions/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.app.Team.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	handle("POST /api/team/missions/{id}/advance", func(w http.ResponseWriter, r *http.Request) {
		if !teamOrigin(w, r) {
			return
		}
		v, err := s.app.AdvanceMission(r.Context(), r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	handle("POST /api/team/missions/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if !teamOrigin(w, r) {
			return
		}
		v, err := s.app.RetryMission(r.Context(), r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	handle("GET /api/team/memory/{project}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.app.Team.Memory(r.Context(), r.PathValue("project"), r.URL.Query().Get("history") == "true")
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, v)
	})
	handle("PUT /api/team/memory/{project}", func(w http.ResponseWriter, r *http.Request) {
		if !teamOrigin(w, r) {
			return
		}
		var v team.Memory
		if !decodeBody(w, r, &v) {
			return
		}
		v.ProjectID = r.PathValue("project")
		out, err := s.app.SaveProjectMemory(r.Context(), v)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, out)
	})
	handle("GET /api/team/projects/{project}/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, app.TeamProjectStatus(r.PathValue("project")))
	})
	handle("GET /api/team/evaluations", func(w http.ResponseWriter, r *http.Request) {
		missions, err := s.app.Team.List(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		type evaluation struct {
			MissionID string         `json:"missionId"`
			Risk      int            `json:"riskLevel"`
			Execution team.Execution `json:"execution"`
		}
		out := []evaluation{}
		for _, m := range missions {
			for _, e := range m.Executions {
				out = append(out, evaluation{m.ID, m.Risk, e})
			}
		}
		writeJSON(w, 200, out)
	})
}

func teamOrigin(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			writeError(w, 403, "cross-origin mutation refused")
			return false
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, 403, "cross-site mutation refused")
		return false
	}
	return true
}

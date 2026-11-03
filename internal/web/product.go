package web

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/app"
	"net/http"
)

func (s *Server) registerProductRoutes() {
	guard := func(pattern string, h http.HandlerFunc) {
		s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && !teamOrigin(w, r) {
				return
			}
			h(w, r)
		})
	}
	guard("GET /api/product/onboarding", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.app.Onboarding(r.Context())
		if e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	guard("POST /api/product/providers", func(w http.ResponseWriter, r *http.Request) {
		var in app.ProviderRegistration
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.app.RegisterProductProvider(r.Context(), in)
		if e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	guard("POST /api/product/providers/{id}/models", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.app.AI.DiscoverModels(r.Context(), r.PathValue("id"))
		if e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	guard("POST /api/product/providers/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		var consent struct {
			Authorize bool `json:"authorize"`
		}
		if !decodeBody(w, r, &consent) {
			return
		}
		if !consent.Authorize {
			writeError(w, 400, "Explicit model call authorization required; API tests consume the configured budget")
			return
		}
		v, e := s.app.ProbeProductProvider(r.Context(), r.PathValue("id"))
		if e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	guard("POST /api/product/providers/{id}/acknowledge-usage", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AcceptReservedCost bool `json:"acceptReservedCost"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if !in.AcceptReservedCost {
			writeError(w, 400, "explicit acceptance of the fully charged reservation required")
			return
		}
		err := s.app.ResumeProviderAfterUncertainUsage(r.Context(), r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"remainingBudgetOnly": true})
	})
	guard("DELETE /api/product/providers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.app.RevokeProductProvider(r.Context(), r.PathValue("id")); e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"revoked": true})
	})
	guard("POST /api/product/projects", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path   string `json:"path"`
			Create bool   `json:"create"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		var value any
		var err error
		if in.Create {
			value, err = s.app.CreateOnboardingProject(r.Context(), in.Path)
		} else {
			value, err = s.app.ImportRepository(r.Context(), in.Path)
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 201, value)
	})
	guard("POST /api/product/missions", func(w http.ResponseWriter, r *http.Request) {
		var in app.SimpleMissionInput
		if !decodeBody(w, r, &in) {
			return
		}
		v, e := s.app.CreateSimpleMission(r.Context(), in)
		if e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 201, v)
	})
	guard("GET /api/product/missions/{id}/delivery", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.app.Delivery(r.Context(), r.PathValue("id"))
		if e != nil {
			s.fail(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	guard("GET /api/product/missions/{id}/delivery.zip", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.app.Delivery(r.Context(), r.PathValue("id"))
		if e != nil {
			s.fail(w, e)
			return
		}
		raw, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			s.fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="delivery-%s.zip"`, v.Mission.ID))
		z := zip.NewWriter(w)
		f, e := z.Create("delivery-evidence.json")
		if e == nil {
			_, e = f.Write(raw)
		}
		if e == nil {
			c, _ := json.MarshalIndent(v.Report.Candidate, "", "  ")
			f, e = z.Create("release-candidate.json")
			if e == nil {
				_, e = f.Write(c)
			}
		}
		if closeErr := z.Close(); e != nil || closeErr != nil {
			s.logger.Printf("delivery archive stream failed")
		}
	})
}

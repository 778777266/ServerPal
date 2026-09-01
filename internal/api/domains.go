package api

import (
	"net/http"
	"strings"

	"github.com/serverpal/serverpal/internal/agent"
	"github.com/serverpal/serverpal/internal/security"
	"github.com/serverpal/serverpal/internal/store"
)

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		domains, err := s.store.Domains(r.Context())
		if err != nil {
			apiError(w, http.StatusInternalServerError, "无法读取域名")
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{"items": domains})
	case http.MethodPost:
		var input struct {
			ProjectID string `json:"projectId"`
			Hostname  string `json:"hostname"`
			HTTPS     bool   `json:"https"`
		}
		if !decodeRequest(w, r, &input, 32*1024) {
			return
		}
		project, err := s.store.Project(r.Context(), input.ProjectID)
		if err != nil {
			apiError(w, http.StatusBadRequest, "项目不存在")
			return
		}
		hostname, err := agent.ValidateHostname(input.Hostname)
		if err != nil {
			apiError(w, http.StatusBadRequest, "域名格式无效")
			return
		}
		id, err := security.RandomID(12)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "无法创建域名")
			return
		}
		domain := store.Domain{ID: id, ProjectID: project.ID, Hostname: hostname, HTTPS: input.HTTPS, Status: "pending"}
		if err := s.store.CreateDomain(r.Context(), domain); err != nil {
			apiError(w, http.StatusConflict, "该域名已经绑定")
			return
		}
		if err := s.agent.ApplyDomain(r.Context(), agent.DomainSpec{
			ID: id, ProjectID: project.ID, Hostname: hostname, Port: project.Port, HTTPS: input.HTTPS,
		}); err != nil {
			_ = s.store.DeleteDomain(r.Context(), id)
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, "domain.bind", hostname, project.Name)
		created, _ := s.store.Domain(r.Context(), id)
		apiJSON(w, http.StatusCreated, created)
	default:
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDomain(w http.ResponseWriter, r *http.Request) {
	id, rest, err := pathID("/api/domains/", r.URL.Path)
	if err != nil {
		apiError(w, http.StatusNotFound, "域名不存在")
		return
	}
	if len(rest) > 0 {
		switch rest[0] {
		case "check":
			s.handleDomainCheck(w, r, id)
		case "apply":
			s.handleDomainApply(w, r, id)
		default:
			apiError(w, http.StatusNotFound, "接口不存在")
		}
		return
	}
	if r.Method != http.MethodDelete {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	domain, err := s.store.Domain(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "域名不存在")
		return
	}
	project, err := s.store.Project(r.Context(), domain.ProjectID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "关联项目不存在")
		return
	}
	if err := s.agent.RemoveDomain(r.Context(), id); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.DeleteDomain(r.Context(), id); err != nil {
		_ = s.agent.ApplyDomain(r.Context(), agent.DomainSpec{
			ID: domain.ID, ProjectID: project.ID, Hostname: domain.Hostname, Port: project.Port, HTTPS: domain.HTTPS,
		})
		apiError(w, http.StatusInternalServerError, "无法删除域名")
		return
	}
	s.audit(r, "domain.remove", domain.Hostname, "")
	apiJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDomainCheck(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	domain, err := s.store.Domain(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "域名不存在")
		return
	}
	status, err := s.agent.CertificateStatus(r.Context(), domain.Hostname)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !domain.HTTPS {
		status.Status = "disabled"
		status.ExpiresAt = nil
	}
	_ = s.store.UpdateDomainStatus(r.Context(), id, status.Status, status.ExpiresAt)
	apiJSON(w, http.StatusOK, status)
}

func (s *Server) handleDomainApply(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	domain, err := s.store.Domain(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "域名不存在")
		return
	}
	project, err := s.store.Project(r.Context(), domain.ProjectID)
	if err != nil {
		apiError(w, http.StatusBadRequest, "关联项目不存在")
		return
	}
	if err := s.agent.ApplyDomain(r.Context(), agent.DomainSpec{
		ID: domain.ID, ProjectID: project.ID, Hostname: domain.Hostname, Port: project.Port, HTTPS: domain.HTTPS,
	}); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.store.UpdateDomainStatus(r.Context(), id, "pending", nil)
	s.audit(r, "domain.apply", strings.ToLower(domain.Hostname), project.Name)
	apiJSON(w, http.StatusOK, map[string]any{"ok": true})
}

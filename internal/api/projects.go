package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/serverpal/serverpal/internal/agent"
	"github.com/serverpal/serverpal/internal/security"
	"github.com/serverpal/serverpal/internal/store"
)

var apiProcessName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type projectInput struct {
	Name       string            `json:"name"`
	RootPath   string            `json:"rootPath"`
	Runtime    string            `json:"runtime"`
	Entry      string            `json:"entry"`
	NPMScript  string            `json:"npmScript"`
	NodeBinary string            `json:"nodeBinary"`
	Port       int               `json:"port"`
	Instances  int               `json:"instances"`
	PM2Name    string            `json:"pm2Name"`
	Env        map[string]string `json:"env"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	metrics, metricsErr := s.agent.Metrics(r.Context())
	projects, err := s.store.Projects(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "无法读取项目")
		return
	}
	domains, err := s.store.Domains(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "无法读取域名")
		return
	}
	processes, processErr := s.agent.Processes(r.Context())
	active := 0
	for _, process := range processes {
		if process.Status == "online" {
			active++
		}
	}
	recent, _ := s.store.AuditLogs(r.Context(), 6)
	warning := ""
	if metricsErr != nil || processErr != nil {
		warning = "系统代理暂时不可用"
	}
	apiJSON(w, http.StatusOK, map[string]any{
		"metrics": metrics,
		"summary": map[string]any{
			"projects": len(projects), "activeProjects": active, "domains": len(domains),
		},
		"processes": processes,
		"recent":    recent,
		"warning":   warning,
	})
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projects, err := s.projectsWithStatus(r)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "无法读取项目")
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{"items": projects})
	case http.MethodPost:
		var input projectInput
		if !decodeRequest(w, r, &input, 512*1024) {
			return
		}
		id, err := security.RandomID(12)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "无法创建项目")
			return
		}
		project, err := s.projectFromInput(id, input)
		if err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.agent.EnsureProjectRoot(r.Context(), project.RootPath); err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.store.CreateProject(r.Context(), project); err != nil {
			apiError(w, http.StatusConflict, "项目名称、PM2 名称或端口已经存在")
			return
		}
		s.audit(r, "project.create", project.Name, project.RootPath)
		apiJSON(w, http.StatusCreated, project)
	default:
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	id, rest, err := pathID("/api/projects/", r.URL.Path)
	if err != nil {
		apiError(w, http.StatusNotFound, "项目不存在")
		return
	}
	if len(rest) > 0 {
		switch rest[0] {
		case "action":
			s.handleProjectAction(w, r, id)
		case "logs":
			s.handleProjectLogs(w, r, id)
		default:
			apiError(w, http.StatusNotFound, "接口不存在")
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		project, err := s.store.Project(r.Context(), id)
		if err != nil {
			apiError(w, http.StatusNotFound, "项目不存在")
			return
		}
		apiJSON(w, http.StatusOK, project)
	case http.MethodPut:
		current, err := s.store.Project(r.Context(), id)
		if err != nil {
			apiError(w, http.StatusNotFound, "项目不存在")
			return
		}
		var input projectInput
		if !decodeRequest(w, r, &input, 512*1024) {
			return
		}
		project, err := s.projectFromInput(id, input)
		if err != nil {
			apiError(w, http.StatusBadRequest, err.Error())
			return
		}
		if project.PM2Name != current.PM2Name {
			apiError(w, http.StatusBadRequest, "PM2 进程名称创建后不能修改")
			return
		}
		if project.RootPath != current.RootPath {
			if err := s.agent.EnsureProjectRoot(r.Context(), project.RootPath); err != nil {
				apiError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		domains, err := s.store.Domains(r.Context())
		if err != nil {
			apiError(w, http.StatusInternalServerError, "无法读取关联域名")
			return
		}
		if err := s.store.UpdateProject(r.Context(), project); err != nil {
			apiError(w, http.StatusConflict, "项目更新失败，名称或端口可能已存在")
			return
		}
		if project.Port != current.Port {
			applied := make([]store.Domain, 0)
			for _, domain := range domains {
				if domain.ProjectID != id {
					continue
				}
				if err := s.agent.ApplyDomain(r.Context(), agent.DomainSpec{
					ID: domain.ID, ProjectID: id, Hostname: domain.Hostname, Port: project.Port, HTTPS: domain.HTTPS,
				}); err != nil {
					_ = s.store.UpdateProject(r.Context(), current)
					for _, previous := range applied {
						_ = s.agent.ApplyDomain(r.Context(), agent.DomainSpec{
							ID: previous.ID, ProjectID: id, Hostname: previous.Hostname, Port: current.Port, HTTPS: previous.HTTPS,
						})
					}
					apiError(w, http.StatusBadRequest, "域名上游更新失败，项目配置已恢复")
					return
				}
				applied = append(applied, domain)
			}
		}
		s.audit(r, "project.update", project.Name, "")
		apiJSON(w, http.StatusOK, project)
	case http.MethodDelete:
		s.deleteProject(w, r, id)
	default:
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleProjectAction(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if !decodeRequest(w, r, &input, 16*1024) {
		return
	}
	project, err := s.store.Project(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "项目不存在")
		return
	}
	request := agent.ProjectAction{Action: input.Action, Project: projectSpec(project)}
	if err := s.agent.ProjectAction(r.Context(), request); err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "project."+input.Action, project.Name, "PM2")
	apiJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleProjectLogs(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	project, err := s.store.Project(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "项目不存在")
		return
	}
	logs, err := s.agent.Logs(r.Context(), project.PM2Name, 500)
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	apiJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request, id string) {
	project, err := s.store.Project(r.Context(), id)
	if err != nil {
		apiError(w, http.StatusNotFound, "项目不存在")
		return
	}
	processes, processErr := s.agent.Processes(r.Context())
	if processErr != nil {
		apiError(w, http.StatusServiceUnavailable, "无法确认 PM2 进程状态，项目记录未删除")
		return
	}
	processExists := false
	for _, process := range processes {
		if process.Name == project.PM2Name {
			processExists = true
			break
		}
	}
	domains, err := s.store.Domains(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "无法确认关联域名，项目记录未删除")
		return
	}
	removedDomains := make([]store.Domain, 0)
	restoreDomains := func() {
		for _, domain := range removedDomains {
			_ = s.agent.ApplyDomain(r.Context(), agent.DomainSpec{
				ID: domain.ID, ProjectID: project.ID, Hostname: domain.Hostname, Port: project.Port, HTTPS: domain.HTTPS,
			})
		}
	}
	for _, domain := range domains {
		if domain.ProjectID == id {
			if err := s.agent.RemoveDomain(r.Context(), domain.ID); err != nil {
				restoreDomains()
				apiError(w, http.StatusBadRequest, "域名配置删除失败，项目记录未删除")
				return
			}
			removedDomains = append(removedDomains, domain)
		}
	}
	if processExists {
		if err := s.agent.ProjectAction(r.Context(), agent.ProjectAction{Action: "delete", Project: projectSpec(project)}); err != nil {
			restoreDomains()
			apiError(w, http.StatusBadRequest, "PM2 进程删除失败，项目记录未删除")
			return
		}
	}
	if err := s.store.DeleteProject(r.Context(), id); err != nil {
		restoreDomains()
		apiError(w, http.StatusInternalServerError, "无法删除项目")
		return
	}
	s.audit(r, "project.delete", project.Name, "项目文件未删除")
	apiJSON(w, http.StatusOK, map[string]any{"ok": true, "filesPreserved": true})
}

func (s *Server) projectsWithStatus(r *http.Request) ([]store.Project, error) {
	projects, err := s.store.Projects(r.Context())
	if err != nil {
		return nil, err
	}
	processes, processErr := s.agent.Processes(r.Context())
	byName := make(map[string]agent.Process, len(processes))
	for _, process := range processes {
		byName[process.Name] = process
	}
	for i := range projects {
		projects[i].Status = "stopped"
		if processErr != nil {
			projects[i].Status = "unavailable"
			continue
		}
		if process, ok := byName[projects[i].PM2Name]; ok {
			projects[i].Status = process.Status
			projects[i].PID = process.PID
			projects[i].CPU = process.CPU
			projects[i].Memory = process.Memory
		}
	}
	return projects, nil
}

func (s *Server) projectFromInput(id string, input projectInput) (store.Project, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 64 {
		return store.Project{}, errors.New("项目名称不能为空且不能超过 64 个字符")
	}
	input.RootPath = filepath.Clean(strings.TrimSpace(input.RootPath))
	if !filepath.IsAbs(input.RootPath) || input.RootPath == "/" {
		return store.Project{}, errors.New("项目目录必须是绝对路径")
	}
	if input.Runtime != "node" && input.Runtime != "npm" {
		return store.Project{}, errors.New("运行方式必须是 Node 入口或 npm script")
	}
	input.Entry = filepath.ToSlash(filepath.Clean(strings.TrimSpace(input.Entry)))
	if input.Runtime == "node" && (input.Entry == "." || input.Entry == "" || filepath.IsAbs(input.Entry) || strings.HasPrefix(input.Entry, "../")) {
		return store.Project{}, errors.New("入口文件必须是项目内的相对路径")
	}
	if input.Runtime == "npm" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,63}$`).MatchString(input.NPMScript) {
		return store.Project{}, errors.New("npm script 名称无效")
	}
	if input.NodeBinary == "" {
		input.NodeBinary = s.cfg.NodeBinary
	}
	if !filepath.IsAbs(input.NodeBinary) {
		return store.Project{}, errors.New("Node 路径必须是绝对路径")
	}
	if input.Port < 1024 || input.Port > 65535 {
		return store.Project{}, errors.New("项目端口必须在 1024 到 65535 之间")
	}
	if input.Instances == 0 {
		input.Instances = 1
	}
	if input.Instances < 1 || input.Instances > 32 {
		return store.Project{}, errors.New("实例数必须在 1 到 32 之间")
	}
	if input.PM2Name == "" {
		input.PM2Name = "sp-" + strings.ToLower(strings.ReplaceAll(id[:10], "_", "-"))
	}
	if !apiProcessName.MatchString(input.PM2Name) {
		return store.Project{}, errors.New("PM2 名称只能包含字母、数字、点、下划线和短横线")
	}
	if len(input.Env) > 100 {
		return store.Project{}, errors.New("环境变量数量不能超过 100")
	}
	if input.Env == nil {
		input.Env = map[string]string{}
	}
	return store.Project{
		ID: id, Name: input.Name, RootPath: input.RootPath, Runtime: input.Runtime,
		Entry: input.Entry, NPMScript: input.NPMScript, NodeBinary: input.NodeBinary,
		Port: input.Port, Instances: input.Instances, PM2Name: input.PM2Name, Env: input.Env,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}

func projectSpec(project store.Project) agent.ProjectSpec {
	return agent.ProjectSpec{
		ID: project.ID, Name: project.Name, RootPath: project.RootPath, Runtime: project.Runtime,
		Entry: project.Entry, NPMScript: project.NPMScript, NodeBinary: project.NodeBinary,
		Port: project.Port, Instances: project.Instances, PM2Name: project.PM2Name, Env: project.Env,
	}
}

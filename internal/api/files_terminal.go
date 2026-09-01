package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/files")
	target := "/v1/files" + suffix
	if suffix == "" || suffix == "/" {
		if r.Method != http.MethodGet {
			apiError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		target = "/v1/files/list"
	}
	allowed := map[string]map[string]bool{
		"/v1/files/list":     {http.MethodGet: true},
		"/v1/files/content":  {http.MethodGet: true, http.MethodPut: true},
		"/v1/files/upload":   {http.MethodPost: true},
		"/v1/files/download": {http.MethodGet: true},
		"/v1/files/mkdir":    {http.MethodPost: true},
		"/v1/files/rename":   {http.MethodPost: true},
		"/v1/files/trash":    {http.MethodPost: true},
	}
	methods, ok := allowed[target]
	if !ok || !methods[r.Method] {
		apiError(w, http.StatusNotFound, "文件接口不存在")
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), r.Method, "http://serverpal"+target+"?"+r.URL.RawQuery, r.Body)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "无法创建文件请求")
		return
	}
	request.ContentLength = r.ContentLength
	request.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	response, err := s.agent.Do(request)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer response.Body.Close()
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Disposition"} {
		if value := response.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
	if response.StatusCode >= 200 && response.StatusCode < 300 && r.Method != http.MethodGet {
		s.audit(r, "file."+strings.TrimPrefix(target, "/v1/files/"), r.URL.Query().Get("path"), "")
	}
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	projectID := r.URL.Query().Get("projectId")
	project, err := s.store.Project(r.Context(), projectID)
	if err != nil {
		apiError(w, http.StatusNotFound, "项目不存在")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	agentSocket, response, err := s.agent.DialTerminal(ctx, project.RootPath)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "无法打开终端")
		return
	}
	defer agentSocket.CloseNow()
	browserSocket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer browserSocket.CloseNow()
	browserSocket.SetReadLimit(64 * 1024)
	s.audit(r, "terminal.open", project.Name, project.RootPath)

	errors := make(chan error, 2)
	go proxyWebSocket(ctx, agentSocket, browserSocket, errors)
	go proxyWebSocket(ctx, browserSocket, agentSocket, errors)
	select {
	case <-ctx.Done():
	case <-errors:
	}
	cancel()
	_ = browserSocket.Close(websocket.StatusNormalClosure, "terminal closed")
	_ = agentSocket.Close(websocket.StatusNormalClosure, "terminal closed")
}

func proxyWebSocket(ctx context.Context, destination, source *websocket.Conn, result chan<- error) {
	for {
		messageType, payload, err := source.Read(ctx)
		if err != nil {
			result <- err
			return
		}
		if err := destination.Write(ctx, messageType, payload); err != nil {
			result <- err
			return
		}
	}
}

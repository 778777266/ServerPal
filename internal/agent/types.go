package agent

import "time"

type Metrics struct {
	CPUPercent    float64 `json:"cpuPercent"`
	CPUCores      int     `json:"cpuCores"`
	Load1         float64 `json:"load1"`
	Load5         float64 `json:"load5"`
	Load15        float64 `json:"load15"`
	MemoryUsed    uint64  `json:"memoryUsed"`
	MemoryTotal   uint64  `json:"memoryTotal"`
	MemoryPercent float64 `json:"memoryPercent"`
	DiskUsed      uint64  `json:"diskUsed"`
	DiskTotal     uint64  `json:"diskTotal"`
	DiskPercent   float64 `json:"diskPercent"`
	UptimeSeconds uint64  `json:"uptimeSeconds"`
}

type ProjectSpec struct {
	ID         string            `json:"id"`
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

type Process struct {
	Name       string  `json:"name"`
	PM2ID      int     `json:"pm2Id"`
	PID        int     `json:"pid"`
	Status     string  `json:"status"`
	CPU        float64 `json:"cpu"`
	Memory     int64   `json:"memory"`
	Restarts   int     `json:"restarts"`
	UptimeUnix int64   `json:"uptimeUnix"`
}

type ProjectAction struct {
	Action  string      `json:"action"`
	Project ProjectSpec `json:"project"`
}

type DomainSpec struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Hostname  string `json:"hostname"`
	Port      int    `json:"port"`
	HTTPS     bool   `json:"https"`
}

type CertificateStatus struct {
	Status    string     `json:"status"`
	Issuer    string     `json:"issuer,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	Mode       string    `json:"mode"`
	Owner      string    `json:"owner"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type FileList struct {
	Root    string      `json:"root"`
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

type SystemStatus struct {
	AgentVersion string `json:"agentVersion"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Node         string `json:"node"`
	PM2          string `json:"pm2"`
	Caddy        string `json:"caddy"`
	PM2Available bool   `json:"pm2Available"`
	CaddyReady   bool   `json:"caddyReady"`
	Terminal     bool   `json:"terminal"`
}

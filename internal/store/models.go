package store

import "time"

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Session struct {
	ID        string
	UserID    string
	Username  string
	TokenHash string
	CSRFToken string
	ExpiresAt time.Time
}

type Project struct {
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
	Status     string            `json:"status,omitempty"`
	PID        int               `json:"pid,omitempty"`
	CPU        float64           `json:"cpu,omitempty"`
	Memory     int64             `json:"memory,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type Domain struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	ProjectName string     `json:"projectName,omitempty"`
	Hostname    string     `json:"hostname"`
	HTTPS       bool       `json:"https"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type AuditLog struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"userId"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
}

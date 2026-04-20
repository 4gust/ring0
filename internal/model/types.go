package model

import "time"

// Status of a managed application process.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusCrashed Status = "crashed"
)

// App is a user-defined process managed by ring0.
type App struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Cmd       string    `json:"cmd"`    // run command, e.g. "node server.js"
	Setup     string    `json:"setup"`  // optional one-shot setup, e.g. "npm install"
	Repo      string    `json:"repo"`   // optional git URL; pulled/cloned on update
	Branch    string    `json:"branch"` // optional git branch (default: main)
	Cwd       string    `json:"cwd"`    // working directory
	Port      int       `json:"port"`   // optional listening port
	Status    Status    `json:"-"`
	PID       int       `json:"-"`
	StartedAt time.Time `json:"-"`
	ExitCode  int       `json:"-"`
}

// Visibility for routes.
type Visibility string

const (
	Public  Visibility = "public"
	Private Visibility = "private"
)

// Route maps an inbound path/host to a target port (or to a redirect URL).
type Route struct {
	ID          string     `json:"id"`
	Path        string     `json:"path"` // e.g. "/api"
	Host        string     `json:"host"` // optional, e.g. "api.local"
	TargetPort  int        `json:"target_port"`
	Visibility  Visibility `json:"visibility"`
	StripPrefix bool       `json:"strip_prefix"` // strip Path before forwarding
	Redirect    string     `json:"redirect"`     // if non-empty, send 308 to this URL (overrides proxy)
}

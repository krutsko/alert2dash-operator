package model

// GrafanaPanelQuery represents a single metric extracted from a Prometheus alert rule
type GrafanaPanelQuery struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

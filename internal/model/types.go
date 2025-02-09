package model

// GrafanaPanelQuery represents a single metric extracted from a Prometheus alert rule
type GrafanaPanelQuery struct {
	Name      string  `json:"name"`
	Query     string  `json:"query"`
	Threshold float64 `json:"threshold"`
	Operator  string  `json:"operator"`
}

type ParsedQueryResult struct {
	Query     string
	Threshold float64
	Operator  string
}

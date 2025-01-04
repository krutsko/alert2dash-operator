package model

// AlertMetric represents a single metric extracted from a Prometheus alert rule
type AlertMetric struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

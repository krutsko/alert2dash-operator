package controller

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Unit tests for query extraction
func TestExtractBaseQuery(t *testing.T) {
	// Simple queries
	simpleQueries := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "greater than operator",
			expr:     "sum(rate(http_requests_total[5m])) > 100",
			expected: []string{"sum(rate(http_requests_total[5m]))"},
		},
		{
			name:     "less than operator",
			expr:     "node_memory_MemAvailable_bytes < 1000000",
			expected: []string{"node_memory_MemAvailable_bytes"},
		},
		{
			name:     "equals operator",
			expr:     `up{job="kubernetes-service-endpoints"} == 0`,
			expected: []string{`up{job="kubernetes-service-endpoints"}`},
		},
		{
			name:     "no operator",
			expr:     "sum(rate(requests_total[5m]))",
			expected: []string{"sum(rate(requests_total[5m]))"},
		},
	}

	// Complex queries
	complexQueries := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "query with by clause",
			expr:     `sum by(code) (rate(http_requests_total[5m])) > 100`,
			expected: []string{"sum by(code) (rate(http_requests_total[5m]))"},
		},
		{
			name:     "query with regex matching",
			expr:     `rate(http_requests{status=~"5.."}[5m]) > 0`,
			expected: []string{`rate(http_requests{status=~"5.."}[5m])`},
		},
		{
			name:     "query with subquery",
			expr:     `max_over_time(rate(http_requests_total[5m])[1h:]) > 100`,
			expected: []string{"max_over_time(rate(http_requests_total[5m])[1h:])"},
		},
	}

	// Complex queries with non-scalar right-hand side
	complexRightHandQueries := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "comparison with calculated value",
			expr:     `sum(metric_a) < sum(metric_b)`,
			expected: []string{""},
		},
		{
			name:     "comparison with complex calculation",
			expr:     `sum(up) < (count(up) + 1) / 2`,
			expected: []string{""},
		},
		{
			name:     "etcd insufficient members query",
			expr:     `sum without (instance) (up{job=~".*etcd.*"} == bool 1) < ((count without (instance) (up{job=~".*etcd.*"}) + 1) / 2)`,
			expected: []string{""},
		},
	}

	// Unsupported queries
	unsupportedQueries := []struct {
		name     string
		expr     string
		expected []string
	}{
		{
			name:     "query with OR operator",
			expr:     `(node_memory_MemAvailable_bytes < 1000000) or (node_memory_MemFree_bytes < 1000000)`,
			expected: []string{""},
		},
		{
			name:     "query with unless operator",
			expr:     `rate(http_requests_total[5m]) > 100 unless on(instance) up == 0`,
			expected: []string{""},
		},
		{
			name:     "etcd insufficient members query",
			expr:     `sum without (instance) (up{job=~".*etcd.*"} == bool 1) < ((count without (instance) (up{job=~".*etcd.*"}) + 1) / 2)`,
			expected: []string{""},
		},
	}

	r := &AlertDashboardReconciler{}

	// Run tests by category
	t.Run("Simple Queries", func(t *testing.T) {
		for _, tt := range simpleQueries {
			t.Run(tt.name, func(t *testing.T) {
				runQueryTest(t, r, tt)
			})
		}
	})

	t.Run("Complex Queries", func(t *testing.T) {
		for _, tt := range complexQueries {
			t.Run(tt.name, func(t *testing.T) {
				runQueryTest(t, r, tt)
			})
		}
	})

	t.Run("Complex Right-Hand Queries", func(t *testing.T) {
		for _, tt := range complexRightHandQueries {
			t.Run(tt.name, func(t *testing.T) {
				runQueryTest(t, r, tt)
			})
		}
	})

	t.Run("Unsupported Queries", func(t *testing.T) {
		for _, tt := range unsupportedQueries {
			t.Run(tt.name, func(t *testing.T) {
				runQueryTest(t, r, tt)
			})
		}
	})
}

func TestExtractBaseQueryInvalidExpr(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{
			name: "invalid prometheus query",
			expr: "sum(rate(invalid metric[5m]) >>",
		},
		{
			name: "malformed query",
			expr: "rate(http_requests{[5m])",
		},
	}

	r := &AlertDashboardReconciler{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := r.extractQuery(&monitoringv1.Rule{
				Expr: intstr.FromString(tt.expr),
			})
			assert.Equal(t, []string{""}, results, "Invalid query should return empty string array")
		})
	}
}

// Helper function to reduce test code duplication
func runQueryTest(t *testing.T, r *AlertDashboardReconciler, tt struct {
	name     string
	expr     string
	expected []string
}) {
	results := r.extractQuery(&monitoringv1.Rule{
		Expr: intstr.FromString(tt.expr),
	})
	for i, result := range results {
		if result == "" && tt.expected[i] == "" {
			continue
		}
		expectedExpr, err := parser.ParseExpr(tt.expected[i])
		require.NoError(t, err, "Failed to parse expected expression")
		assert.Equal(t, expectedExpr.String(), result,
			"Extracted query does not match expected")
	}
}

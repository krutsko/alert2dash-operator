package controller

import (
	"testing"

	"github.com/krutsko/alert2dash-operator/internal/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit tests for query extraction
func TestExtractBaseQuery(t *testing.T) {
	// Simple queries
	simpleQueries := []struct {
		name     string
		expr     string
		expected []model.ParsedQueryResult
	}{
		{
			name:     "greater than operator",
			expr:     "sum(rate(http_requests_total[5m])) > 100",
			expected: []model.ParsedQueryResult{{Query: "sum(rate(http_requests_total[5m]))", Threshold: 100, Operator: "gt"}},
		},
		{
			name:     "less than operator",
			expr:     "node_memory_MemAvailable_bytes < 1000000",
			expected: []model.ParsedQueryResult{{Query: "node_memory_MemAvailable_bytes", Threshold: 1000000, Operator: "lt"}},
		},
		{
			name:     "equals operator",
			expr:     `up{job="kubernetes-service-endpoints"} == 0`,
			expected: []model.ParsedQueryResult{{Query: `up{job="kubernetes-service-endpoints"}`, Threshold: 0, Operator: "gt"}},
		},
		{
			name:     "no operator",
			expr:     "sum(rate(requests_total[5m]))",
			expected: []model.ParsedQueryResult{},
		},
	}

	// Complex queries
	complexQueries := []struct {
		name     string
		expr     string
		expected []model.ParsedQueryResult
	}{
		{
			name:     "query with by clause",
			expr:     `sum by(code) (rate(http_requests_total[5m])) > 100`,
			expected: []model.ParsedQueryResult{{Query: "sum by(code) (rate(http_requests_total[5m]))", Threshold: 100, Operator: "gt"}},
		},
		{
			name:     "query with regex matching",
			expr:     `rate(http_requests{status=~"5.."}[5m]) > 0`,
			expected: []model.ParsedQueryResult{{Query: `rate(http_requests{status=~"5.."}[5m])`, Threshold: 0, Operator: "gt"}},
		},
		{
			name:     "query with subquery",
			expr:     `max_over_time(rate(http_requests_total[5m])[1h:]) > 100`,
			expected: []model.ParsedQueryResult{{Query: "max_over_time(rate(http_requests_total[5m])[1h:])", Threshold: 100, Operator: "gt"}},
		},
		{
			name:     "query_with_parentheses",
			expr:     `(rate(http_requests{status=~"5.."}[5m]) > 0)`,
			expected: []model.ParsedQueryResult{{Query: `rate(http_requests{status=~"5.."}[5m])`, Threshold: 0, Operator: "gt"}},
		},
		{
			name:     "query_with_arithmetic_operation",
			expr:     `(rate(http_requests{status=~"5.."}[5m]) + 100)`,
			expected: []model.ParsedQueryResult{},
		},
	}

	// Complex queries with non-scalar right-hand side
	complexRightHandQueries := []struct {
		name     string
		expr     string
		expected []model.ParsedQueryResult
	}{
		{
			name:     "comparison with calculated value",
			expr:     `sum(metric_a) < sum(metric_b)`,
			expected: []model.ParsedQueryResult{},
		},
		{
			name:     "comparison with complex calculation",
			expr:     `sum(up) < (count(up) + 1) / 2`,
			expected: []model.ParsedQueryResult{},
		},
		{
			name:     "etcd insufficient members query",
			expr:     `sum without (instance) (up{job=~".*etcd.*"} == bool 1) < ((count without (instance) (up{job=~".*etcd.*"}) + 1) / 2)`,
			expected: []model.ParsedQueryResult{},
		},
	}

	// Unsupported queries
	unsupportedQueries := []struct {
		name     string
		expr     string
		expected []model.ParsedQueryResult
	}{
		{
			name:     "query with OR operator",
			expr:     `(node_memory_MemAvailable_bytes < 1000000) or (node_memory_MemFree_bytes < 1000000)`,
			expected: []model.ParsedQueryResult{},
		},
		{
			name:     "query with unless operator",
			expr:     `rate(http_requests_total[5m]) > 100 unless on(instance) up == 0`,
			expected: []model.ParsedQueryResult{},
		},
		{
			name:     "etcd insufficient members query",
			expr:     `sum without (instance) (up{job=~".*etcd.*"} == bool 1) < ((count without (instance) (up{job=~".*etcd.*"}) + 1) / 2)`,
			expected: []model.ParsedQueryResult{},
		},
	}

	// Scalar comparison queries
	scalarComparisonQueries := []struct {
		name     string
		expr     string
		expected []model.ParsedQueryResult
	}{
		{
			name:     "scalar on right side",
			expr:     "node_memory_MemAvailable_bytes < 1000000",
			expected: []model.ParsedQueryResult{{Query: "node_memory_MemAvailable_bytes", Threshold: 1000000, Operator: "lt"}},
		},
		{
			name:     "scalar on left side",
			expr:     "100 < rate(http_requests_total[5m])",
			expected: []model.ParsedQueryResult{{Query: "rate(http_requests_total[5m])", Threshold: 100, Operator: "gt"}},
		},
		{
			name:     "both sides non-scalar",
			expr:     "rate(metric_a[5m]) > rate(metric_b[5m])",
			expected: []model.ParsedQueryResult{{}},
		},
		{
			name:     "both sides scalar",
			expr:     "100 < 200",
			expected: []model.ParsedQueryResult{{}},
		},
		{
			name:     "complex right side",
			expr:     "metric_a > sum(metric_b) / count(metric_b)",
			expected: []model.ParsedQueryResult{},
		},
		{
			name:     "container memory usage with division and grouping",
			expr:     `(container_memory_working_set_bytes{container="sidecar"}) / on (namespace, pod, container) (kube_pod_container_resource_requests{container="sidecar",job="kube-state-metrics",resource="memory"}) > 0.6`,
			expected: []model.ParsedQueryResult{{Query: `(container_memory_working_set_bytes{container="sidecar"}) / on (namespace, pod, container) (kube_pod_container_resource_requests{container="sidecar",job="kube-state-metrics",resource="memory"})`, Threshold: 0.6, Operator: "gt"}},
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

	t.Run("Scalar Comparison Queries", func(t *testing.T) {
		for _, tt := range scalarComparisonQueries {
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
			results := r.extractQuery(tt.expr)
			assert.Equal(t, []model.ParsedQueryResult{}, results, "Invalid query should return empty string array")
		})
	}
}

// Helper function to reduce test code duplication
func runQueryTest(t *testing.T, r *AlertDashboardReconciler, tt struct {
	name     string
	expr     string
	expected []model.ParsedQueryResult
}) {
	results := r.extractQuery(tt.expr)
	for i, result := range results {

		// query
		expectedExpr, err := parser.ParseExpr(tt.expected[i].Query)
		require.NoError(t, err, "Failed to parse expected expression")
		assert.Equal(t, expectedExpr.String(), result.Query,
			"Extracted query does not match expected"+tt.name)

		// threshold
		assert.Equal(t, tt.expected[i].Threshold, result.Threshold,
			"Extracted threshold does not match expected"+tt.name)

		// operator
		assert.Equal(t, tt.expected[i].Operator, result.Operator,
			"Extracted operator does not match expected"+tt.name)
	}
}

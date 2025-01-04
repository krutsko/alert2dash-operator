package controller

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-jsonnet"
	monitoringv1alpha1 "github.com/krutsko/alert2dash-operator/api/v1alpha1"
	"github.com/krutsko/alert2dash-operator/internal/model"
)

// defaultDashboardGenerator implements DashboardGenerator
type defaultDashboardGenerator struct {
	templates embed.FS
	log       logr.Logger
}

// embedImporter implements jsonnet.Importer interface for embedded files
type embedImporter struct {
	templates embed.FS
}

func (i *embedImporter) Import(importedFrom, importedPath string) (contents jsonnet.Contents, foundAt string, err error) {
	var fullPath string
	if strings.HasPrefix(importedPath, "vendor/") {
		fullPath = filepath.Join("templates", importedPath)
	} else {
		if importedFrom != "" {
			dir := filepath.Dir(importedFrom)
			importedPath = filepath.Join(dir, importedPath)
		}
		fullPath = filepath.Join("templates", importedPath)
	}

	content, err := i.templates.ReadFile(fullPath)
	if err != nil {
		return jsonnet.Contents{}, "", fmt.Errorf("failed to read template file %s: %w", fullPath, err)
	}

	return jsonnet.MakeContents(string(content)), importedPath, nil
}

func (g *defaultDashboardGenerator) GenerateDashboard(dashboard *monitoringv1alpha1.AlertDashboard, metrics []model.AlertMetric) ([]byte, error) {
	g.log.Info("Generating dashboard", "name", dashboard.Name)
	var err error

	vm := jsonnet.MakeVM()
	vm.Importer(&embedImporter{templates: g.templates})

	// Marshal metrics to JSON
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metrics: %w", err)
	}

	// Set external variables
	vm.ExtVar("title", dashboard.Name)
	vm.ExtVar("metrics", string(metricsJSON))

	// Get the template content
	var templateContent string

	if dashboard.Spec.CustomJsonnetTemplate != "" {
		templateContent = dashboard.Spec.CustomJsonnetTemplate
		g.log.Info("Using custom template from spec")
	} else {
		template, err := g.templates.ReadFile("templates/dashboard.jsonnet")
		if err != nil {
			return nil, fmt.Errorf("failed to read default template: %w", err)
		}
		templateContent = string(template)
	}

	// Evaluate the template
	result, err := vm.EvaluateSnippet("dashboard.jsonnet", templateContent)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate template: %w", err)
	}

	// Parse and format the JSON
	var prettyJSON map[string]interface{}
	if err := json.Unmarshal([]byte(result), &prettyJSON); err != nil {
		return nil, fmt.Errorf("failed to parse generated JSON: %w", err)
	}

	prettyResult, err := json.MarshalIndent(prettyJSON, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format JSON: %w", err)
	}

	g.log.V(1).Info("Successfully generated dashboard", "name", dashboard.Name, "jsonSize", len(prettyResult))
	return prettyResult, nil
}

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/google/go-jsonnet"
)

//go:embed templates
var templates embed.FS

// Custom importer for embedded files
type embedImporter struct {
	templates embed.FS
}

func (i *embedImporter) Import(importedFrom, importedPath string) (contents jsonnet.Contents, foundAt string, err error) {
	// If the path starts with vendor, it's relative to templates directory
	var fullPath string
	if strings.HasPrefix(importedPath, "vendor/") {
		fullPath = filepath.Join("templates", importedPath)
	} else {
		// Handle relative imports
		if importedFrom != "" {
			dir := filepath.Dir(importedFrom)
			importedPath = filepath.Join(dir, importedPath)
		}
		fullPath = filepath.Join("templates", importedPath)
	}

	// Read the file
	content, err := i.templates.ReadFile(fullPath)
	if err != nil {
		log.Printf("Failed to read file %s: %v", fullPath, err)
		return jsonnet.Contents{}, "", err
	}

	return jsonnet.MakeContents(string(content)), importedPath, nil
}

func main() {
	// Create metrics data
	metrics := []map[string]string{
		{
			"name":  "CPU Usage",
			"query": "rate(node_cpu_seconds_total{mode='system'}[5m])",
		},
		{
			"name":  "Memory Usage",
			"query": "node_memory_MemTotal_bytes - node_memory_MemFree_bytes",
		},
		{
			"name":  "Disk Usage",
			"query": "node_filesystem_size_bytes{mountpoint='/'} - node_filesystem_free_bytes{mountpoint='/'}",
		},
	}

	// Convert metrics to JSON
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		log.Fatalf("Failed to marshal metrics: %v", err)
	}

	// Create Jsonnet VM
	vm := jsonnet.MakeVM()

	// Set external variables
	vm.ExtVar("title", "System Metrics Dashboard")
	vm.ExtVar("metrics", string(metricsJSON))

	// Set the custom importer
	vm.Importer(&embedImporter{templates: templates})

	// Read and evaluate the template
	template, err := templates.ReadFile("templates/dashboard.jsonnet")
	if err != nil {
		log.Fatalf("Failed to read template: %v", err)
	}

	// Evaluate the template
	result, err := vm.EvaluateSnippet("dashboard.jsonnet", string(template))
	if err != nil {
		log.Fatalf("Failed to evaluate template: %v", err)
	}

	// Pretty print the result
	var prettyJSON map[string]interface{}
	if err := json.Unmarshal([]byte(result), &prettyJSON); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	prettyResult, err := json.MarshalIndent(prettyJSON, "", "  ")
	if err != nil {
		log.Fatalf("Failed to format JSON: %v", err)
	}

	fmt.Println(string(prettyResult))
}

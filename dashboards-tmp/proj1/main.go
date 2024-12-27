package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/go-jsonnet"
	"github.com/google/go-jsonnet/ast"
)

//go:embed templates/*.jsonnet
var templates embed.FS

// Custom importer for embedded files
type embedImporter struct{}

func (i *embedImporter) Import(importedFrom, importedPath string) (contents jsonnet.Contents, foundAt string, err error) {
	content, err := templates.ReadFile("templates/" + importedPath)
	if err != nil {
		return jsonnet.Contents{}, "", err
	}
	return jsonnet.MakeContents(string(content)), importedPath, nil
}

// Define a custom native function for timestamp
func timestamp(vm *jsonnet.VM) {
	vm.NativeFunction(&jsonnet.NativeFunction{
		Name:   "timestamp",
		Params: ast.Identifiers{},
		Func: func(args []interface{}) (interface{}, error) {
			return time.Now().Unix(), nil
		},
	})
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
	}

	// Convert metrics to JSON
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		log.Fatalf("Failed to marshal metrics: %v", err)
	}

	// Create Jsonnet VM
	vm := jsonnet.MakeVM()

	// Add custom native functions
	timestamp(vm)

	// Set external variables
	vm.ExtVar("title", "System Metrics Dashboard")
	vm.ExtVar("metrics", string(metricsJSON))

	// Set the custom importer
	vm.Importer(&embedImporter{})

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

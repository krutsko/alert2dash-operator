<div align="center">

# Alert2Dash Operator

[![codecov](https://codecov.io/gh/krutsko/alert2dash-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/krutsko/alert2dash-operator)

</div>

The Alert2Dash Operator is a Kubernetes operator that automatically generates Grafana dashboards from Prometheus alerting rules. It simplifies the process of visualizing and monitoring your alerts by creating customized dashboards.

> **Note:** This operator is designed to work alongside:
> - [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator) - for managing Prometheus rules and alerts
> - [grafana-operator](https://github.com/grafana-operator/grafana-operator) - for managing Grafana dashboards

## How it works

Here's the diagram with Grafana Dashboards as output boxes from Grafana Operator:

```
                                                                                
┌─────────────────┐                                                            
│ PrometheusRule  │                                                            
│ labels:         │──┐      ┌───────────────────┐    ┌────────────────┐       ┌─────────────┐      ┌────────────────┐
│  team: foo      │  │      │  AlertDashboard-1 │    │  ConfigMap-1   │       │   Grafana   │      │ Dashboard      │
└─────────────────┘  ├────▶ │  selector:        │──▶ │  (dash.json)   │──────▶│  Operator   │─────▶│ "Team Foo"     │
┌─────────────────┐  │      │    team: foo      │    └────────────────┘       │             │      └────────────────┘
│ PrometheusRule  │──┘      └───────────────────┘                             │             │
│ labels:         │                                                           │             │
│  team: foo      │                                                           │             │      ┌────────────────┐
└─────────────────┘                                                           │             │      │ Dashboard      │
                                                                              │             │────▶ │ "Team Bar"     │
┌─────────────────┐                                                           │             │      └────────────────┘
│ PrometheusRule  │──┐      ┌───────────────────┐    ┌────────────────┐       │             │
│ labels:         │  │      │  AlertDashboard-2 │    │  ConfigMap-2   │       │             │
│  team: bar      │  ├────▶ │  selector:        │──▶ │  (dash.json)   │──────▶│             │
└─────────────────┘  │      │    team: bar      │    └────────────────┘       │             │
┌─────────────────┐  │      └───────────────────┘                             │             │
│ PrometheusRule  │──┘                                                        └─────────────┘
│ labels:         │
│  team: bar      │
└─────────────────┘

```

## Features

- Automatic dashboard generation from PrometheusRules
- Configurable dashboard layouts and panels
- Support for custom Jsonnet templates
- Grafana folder organization
- Kubernetes-native deployment and configuration

## Installation

**Option 1: Helm Chart**

Deploy the Alert2Dash Operator easily in your cluster using Helm:

```bash
helm upgrade -i alert2dash oci://ghcr.io/krutsko/helm-charts/alert2dash-operator -n alert2dash-system
```

**Option 2: Using the installer**

Users can just run kubectl to install the project, i.e.:

```sh
# Latest release
kubectl apply -f https://github.com/krutsko/alert2dash-operator/releases/download/latest/install.yaml

# Specific version
kubectl apply -f https://github.com/krutsko/alert2dash-operator/releases/download/vX.Y.Z/install.yaml
```

## Development

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Running from source

1. Install Instances of Custom Resources:

CRDs:
```sh
kubectl apply -f config/crd/bases
```

All:
```sh
kubectl apply -f config/samples/
```

AlertDashboard:
```sh
kubectl apply -f config/samples/monitoring_v1alpha1_alertdashboard.yaml
```

### To Deploy on the cluster
**Build and push your image to the location specified by `
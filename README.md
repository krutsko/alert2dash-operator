<div align="center">

# Alert2Dash Operator

[![codecov](https://codecov.io/gh/krutsko/alert2dash-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/krutsko/alert2dash-operator)
</div>

Alert2Dash is a Kubernetes Operator that automatically generates Grafana dashboards from Prometheus alerting rules. It simplifies the process of visualizing and monitoring your alerts by creating customized dashboards that stay synchronized with your alerting definitions.

## Key Features

- **Automatic Dashboard Generation**: Convert PrometheusRules directly into meaningful Grafana dashboards
- **Zero-Drift Guarantee**: Dashboards automatically update when alert definitions change 
- **Template Support**: Use custom Jsonnet templates for advanced dashboard customization: panel sizes, and grouping etc.
- **Kubernetes-Native**: Fully integrated with Kubernetes resource model and lifecycle management

> **Note:** This operator is designed to work alongside:
> - [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator) - for managing Prometheus rules and alerts
> - [grafana-operator](https://github.com/grafana-operator/grafana-operator) - for managing Grafana dashboards


## How It Works

Alert2Dash Operator follows a Kubernetes-native approach:

1. It monitors PrometheusRule resources in your cluster
2. When an AlertDashboard custom resource is created, it uses label selectors to identify relevant PrometheusRules
3. It extracts metrics from alert expressions and analyzes their query patterns
4. It generates dashboard JSON using predefined template
5. The generated JSON is stored in ConfigMaps with proper labels
6. The Grafana Operator detects these ConfigMaps and creates actual dashboards in Grafana


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



## AlertDashboard Custom Resource Definition

Here's an example of an AlertDashboard custom resource that creates a dashboard for a specific team:

```yaml
apiVersion: monitoring.krutsko.com/v1alpha1
kind: AlertDashboard
metadata:
  name: team-foo
  namespace: monitoring
spec:
  # Selector for the team's alerts
  metadataLabelSelector:
    matchLabels:
      team: foo
  # Selector for the alert rules to include
  ruleLabelSelector:
    matchLabels:
      severity: critical
  dashboardConfig:
    configMapNamePrefix: "grafana-dashboard"
```

This example:
- Creates a dashboard named "team-foo" in the monitoring namespace
- Selects alerts specifically for Team Foo using the `team` label
- Includes only critical severity alerts
- Creates ConfigMaps with the prefix "grafana-dashboard"

The operator will automatically:
1. Find all PrometheusRules with matching labels
2. Extract metrics from alert expressions
3. Generate a Grafana dashboard
4. Create a ConfigMap with the dashboard JSON
5. The Grafana Operator will then create the actual dashboard in Grafana

## Installation

**Option 1: Helm Chart**

Deploy the Alert2Dash Operator easily in your cluster using Helm:

```bash
helm upgrade -i alert2dash oci://ghcr.io/krutsko/helm-charts/alert2dash-operator -n alert2dash-system
```

**Option 2: Using the installer**

Users can just run kubectl to install the project, i.e.:

- Latest release

```sh
kubectl apply -f https://github.com/krutsko/alert2dash-operator/releases/download/latest/install.yaml
```

- Specific version

```sh
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
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=ghcr.io/krutsko/alert2dash-operator:tag
```

Build image without pushing it to the registry:
```sh
make docker-build IMG=ghcr.io/krutsko/alert2dash-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don't work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=ghcr.io/krutsko/alert2dash-operator:tag
```

**Load docker image to load kind cluster:**

```sh
kind load docker-image ghcr.io/krutsko/alert2dash-operator:tag -n <cluster>
```

**Load docker image to load kind cluster:**

```sh
kind load docker-image <some-registry>/alert2dash-operator:tag -n v1.23
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### Load helm chart

```sh
helm pull oci://ghcr.io/krutsko/helm-charts/alert2dash-operator
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=ghcr.io/krutsko/alert2dash-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/alert2dash-operator/<tag or branch>/dist/install.yaml
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

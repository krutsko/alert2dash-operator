<div align="center">

# Alert2Dash Operator

</div>

The Alert2Dash Operator is a Kubernetes operator that automatically generates Grafana dashboards from Prometheus alerting rules. It simplifies the process of visualizing and monitoring your alerts by creating customized dashboards. (TODO: that include alert overviews, history, and time series graphs)

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
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/alert2dash-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/alert2dash-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

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
make build-installer IMG=<some-registry>/alert2dash-operator:tag
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

## License

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.


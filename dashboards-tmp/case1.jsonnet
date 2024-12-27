local grafana = import 'vendor/grafonnet-lib/grafonnet/grafana.libsonnet';
local dashboard = grafana.dashboard;
local row = grafana.row;
local prometheus = grafana.prometheus;
local graphPanel = grafana.graphPanel;  // Use graphPanel instead of panel

dashboard.new(
  'My Prometheus Dashboard',
  tags=['prometheus'],
  time_from='now-1h'
)
.addRow(
  row.new()
  .addPanel(
    graphPanel.new(
      'HTTP Request Rate',
      datasource='Prometheus',
    ).addTarget(
      prometheus.target(
        'rate(http_requests_total[5m])',
        legendFormat='{{method}} {{path}}'
      )
    )
  )
)
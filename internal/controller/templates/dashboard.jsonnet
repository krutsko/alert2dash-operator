local grafana = import 'vendor/grafonnet-lib/grafonnet/grafana.libsonnet';
local dashboard = grafana.dashboard;
local graphPanel = grafana.graphPanel;
local prometheus = grafana.prometheus;
local template = grafana.template;
local row = grafana.row;

dashboard.new(
  std.extVar('title'),
  tags=['generated', 'prometheus'],
  time_from='now-1h',
  timezone='browser',
  refresh='10s',
  editable=true,
)
.addTemplate(
  template.new(
    'instance',
    'Prometheus',  // Use direct datasource name instead of variable
    'label_values(up, instance)',
    label='Instance',
    refresh=2,
  )
)
.addRow(
  row.new()
  .addPanels(
    std.map(
      function(metric)
        graphPanel.new(
          title=metric.name,
          datasource='Prometheus',  // Use direct datasource name
          description=metric.name,
        )
        .addTarget(
          prometheus.target(
            metric.query,
            legendFormat='{{instance}} ' + metric.name,
          )
        ),
      std.parseJson(std.extVar('metrics'))
    )
  )
)
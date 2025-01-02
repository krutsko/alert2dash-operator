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
.addPanels(  // Changed from addRow to addPanels
  std.mapWithIndex(
    function(index, metric)
      graphPanel.new(
        title=metric.name,
        datasource={
          type: 'prometheus',
          uid: 'prometheus'
        },
        description=metric.name,
      )
      .addTarget(
        prometheus.target(
          metric.query,
          legendFormat=metric.name,
        )
      )
      + {
        gridPos: {
          h: 8,
          w: 24,
          x: 0,
          y: index * 8
        },
        bars: true,
        lines: false,
        fill: 1,
        transparent: true,
        legend: {
          alignAsTable: true,
          avg: true,
          max: true,
          min: true,
          show: true,
          values: true
        }
      },
    std.parseJson(std.extVar('metrics'))
  )
)
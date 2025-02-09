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
  template.datasource(
    'prometheus_ds',
    'prometheus',
    '.*',
    label='Data Source',
  )
)
.addPanels(
  std.mapWithIndex(
    function(index, metric)
      graphPanel.new(
        title=metric.name,
        datasource={
          type: 'prometheus',
          uid: '${prometheus_ds}'
        },
      )
      .addTarget(
        prometheus.target(
          metric.query,
        )
      )
      + {
        gridPos: {
          h: 8,
          w: 24,
          x: 0,
          y: index * 8
        },
        thresholds: [
            {
              colorMode: 'critical',
              fill: true,
              line: true,
              op: metric.operator,  // 'gt' or 'lt'
              value: metric.threshold,
              yaxis: 'right'
            }
        ]
      },
    std.parseJson(std.extVar('metrics'))
  )
)
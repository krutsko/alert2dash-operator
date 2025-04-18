local grafana = import 'vendor/grafonnet-lib/grafonnet/grafana.libsonnet';
local dashboard = grafana.dashboard;
local graphPanel = grafana.graphPanel;
local prometheus = grafana.prometheus;
local template = grafana.template;
local row = grafana.row;

// Generate a short hash of the title for the UID
local titleHash = std.md5(std.extVar('title'));
local dashboardUid = std.substr(titleHash, 0, 40); // 40 chars total for the UID

dashboard.new(
  std.extVar('title'),
  uid=dashboardUid,
  tags=['generated', 'alert2dash'],
  time_from='now-1h',
  timezone='browser',
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
        thresholds: {
          steps: [
            {
              color: 'green',
              value: null
            },
            {
              color: 'red',
              value: metric.threshold
            }
          ],
          mode: 'absolute'
        },
        fieldConfig: {
          defaults: {
            thresholds: {
              mode: 'absolute',
              steps: [
                {
                  color: 'green',
                  value: null
                },
                {
                  color: 'red',
                  value: metric.threshold
                }
              ]
            },
            custom: {
              thresholdsStyle: {
                mode: 'line+area'
              }
            }
          }
        },
        type: 'timeseries',  // Use timeseries instead of graph panel type
      },
    std.parseJson(std.extVar('metrics'))
  )
)
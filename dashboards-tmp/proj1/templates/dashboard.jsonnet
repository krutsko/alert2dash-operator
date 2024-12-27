local grafana = import 'vendor/grafonnet-lib/grafonnet/grafana.libsonnet';
local dashboard = grafana.dashboard;
local graphPanel = grafana.graphPanel;
local prometheus = grafana.prometheus;
local template = grafana.template;

{
  dashboard:
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
        '${DS_PROMETHEUS}',
        'label_values(up, instance)',
        label='Instance',
        refresh='time',
      )
    )
    .addPanels(
      std.map(
        function(metric)
          graphPanel.new(
            title=metric.name,
            datasource='${DS_PROMETHEUS}',
            description=metric.name,
          )
          .addTarget(
            prometheus.target(
              metric.query,
              legendFormat=metric.name,
            )
          ),
        std.parseJson(std.extVar('metrics'))
      )
    ),
}
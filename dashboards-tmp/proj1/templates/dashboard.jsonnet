{
  // Template function that creates a dashboard
  local dashboard(title, metrics) = {
    title: title,
    metrics: metrics,
    panels: [
      {
        name: metric.name,
        query: metric.query,
        type: 'graph',
      }
      for metric in metrics
    ],
  },

  // Example usage
  dashboard: dashboard(
    std.extVar('title'),
    std.parseJson(std.extVar('metrics'))
  )
}
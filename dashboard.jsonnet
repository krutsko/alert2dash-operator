// dashboard.jsonnet
local dashboard = {
    new(title, rules, config):: {
        local cfg = if config == null then {} else config,
        local panels = if cfg.panels == null then {} else cfg.panels,
        
        title: title,
        editable: true,
        panels: [],
        templating: {
            list: []
        },
        time: {
            from: 'now-6h',
            to: 'now'
        },
        refresh: '1m',
        _panelY:: 0,  // Track current Y position

        // Add overview panel if enabled
        addAlertsOverview(rules):: if panels.alertsOverview then self + {
            _panelY:: 8,
            panels+: [{
                title: 'Alerts Overview',
                type: 'table',
                gridPos: { h: 8, w: 24, x: 0, y: 0 },
                datasource: { type: 'prometheus', uid: 'prometheus' },
                targets: [{
                    expr: 'ALERTS',
                    instant: true,
                    refId: 'A'
                }],
                transformations: [{
                    id: 'organize',
                    options: {
                        excludeByName: {},
                        indexByName: {},
                        renameByName: {
                            alertname: 'Alert',
                            alertstate: 'State',
                            severity: 'Severity'
                        }
                    }
                }]
            }]
        } else self,

        // Add time series panels if enabled
        addTimeSeriesGraphs(rules):: if panels.timeSeriesGraphs then self + {
            local ruleArray = if std.type(rules) == 'array' then rules else [],
            local startY = self._panelY,
            _panelY:: startY + (std.length(ruleArray) / 2) * 8,
            panels+: std.mapWithIndex(function(i, rule) {
                title: rule.alert,
                type: 'timeseries',
                gridPos: {
                    h: 8,
                    w: 12,
                    x: (i % 2) * 12,
                    y: std.floor(i / 2) * 8 + startY
                },
                datasource: { type: 'prometheus', uid: 'prometheus' },
                description: if std.objectHas(rule, 'annotations') then rule.annotations.description else '',
                targets: [{
                    expr: rule.expr,
                    legendFormat: rule.alert,
                    refId: 'A'
                }],
                fieldConfig: {
                    defaults: {
                        custom: {
                            drawStyle: 'line',
                            lineInterpolation: 'linear',
                            spanNulls: false
                        },
                        thresholds: {
                            mode: 'absolute',
                            steps: [
                                { value: null, color: 'green' },
                                { value: 0.7, color: 'red' }
                            ]
                        }
                    }
                }
            }, ruleArray)
        } else self,

        // Add alert history if enabled
        addAlertHistory():: if panels.alertHistory then self + {
            panels+: [{
                title: 'Alert History',
                type: 'timeseries',
                gridPos: {
                    h: 8,
                    w: 24,
                    x: 0,
                    y: self._panelY
                },
                datasource: { type: 'prometheus', uid: 'prometheus' },
                targets: [{
                    expr: 'changes(ALERTS{alertstate="firing"}[24h])',
                    legendFormat: '{{alertname}}',
                    refId: 'A'
                }]
            }]
        } else self,

        // Add template variables
        addVariables(vars):: self + {
            local varArray = if std.type(vars) == 'array' then vars else [],
            templating+: {
                list+: std.map(function(v)
                    if v.type == 'query' then {
                        name: v.name,
                        type: 'query',
                        datasource: { type: 'prometheus', uid: 'prometheus' },
                        query: v.query,
                        refresh: 2,
                        sort: 1
                    } else if v.type == 'custom' then {
                        name: v.name,
                        type: 'custom',
                        query: std.join(',', v.values),
                        current: { selected: true, text: v.values[0], value: v.values[0] }
                    }, varArray)
            }
        }
    }
};

// Sample data for testing
local sampleRules = [
    {
        alert: 'HighLatency',
        expr: 'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 1',
        annotations: {
            description: 'High latency detected'
        }
    },
    {
        alert: 'HighErrorRate',
        expr: 'rate(http_requests_total{status=~"5.."}[5m]) / rate(http_requests_total[5m]) > 0.1',
        annotations: {
            description: 'High error rate detected'
        }
    }
];

local sampleConfig = {
    panels: {
        alertsOverview: true,
        timeSeriesGraphs: true,
        alertHistory: true
    }
};

local sampleVariables = [
    {
        name: 'namespace',
        type: 'query',
        query: 'label_values(namespace)'
    },
    {
        name: 'severity',
        type: 'custom',
        values: ['critical', 'warning', 'info']
    }
];

// Generate dashboard with sample data
dashboard.new('Sample Alerts Dashboard', sampleRules, sampleConfig)
    .addAlertsOverview(sampleRules)
    .addTimeSeriesGraphs(sampleRules)
    .addAlertHistory()
    .addVariables(sampleVariables)
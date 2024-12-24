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

        // Add overview panel if enabled
        addAlertsOverview(rules):: if panels.alertsOverview then self + {
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
            panels+: std.mapWithIndex(function(i, rule) {
                title: rule.alert,
                type: 'timeseries',
                gridPos: {
                    h: 8,
                    w: 12,
                    x: (i % 2) * 12,
                    y: std.floor(i / 2) * 8 + (if panels.alertsOverview then 8 else 0)
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
                    y: std.length(self.panels) * 8
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

local rules = std.parseJson('%s');
local config = std.parseJson('%s');
local variables = if std.length('%s') > 0 then std.parseJson('%s') else [];

dashboard.new('%s', rules, config)
    .addAlertsOverview(rules)
    .addTimeSeriesGraphs(rules)
    .addAlertHistory()
    .addVariables(variables)
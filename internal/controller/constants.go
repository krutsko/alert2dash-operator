package controller

import "time"

const (
	// LabelExcludeRule is used to exclude rules from dashboard generation
	LabelExcludeRule = "alert2dash-exclude-rule"
	RequeueDelay     = 10 * time.Second
)

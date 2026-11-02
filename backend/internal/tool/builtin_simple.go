package tool

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/expr-lang/expr"
)

// RegisterSimple installs calculation and date/time handlers that require no
// external service configuration.
func RegisterSimple(r Registry) {
	r.Register("calculator", runCalculator)
	r.Register("datetime", runDatetime)
}

// runCalculator evaluates a numeric expression with expr built-ins disabled.
// The result must be float64; integral values omit decimals and fractional values
// use six decimal places for predictable model-readable output.
func runCalculator(_ context.Context, args map[string]any, _ string) (string, error) {
	expression, _ := args["expression"].(string)
	if expression == "" {
		return "", fmt.Errorf("expression is required")
	}

	program, err := expr.Compile(expression, expr.AsFloat64(), expr.DisableAllBuiltins())
	if err != nil {
		return "", fmt.Errorf("invalid expression: %v", err)
	}
	out, err := expr.Run(program, nil)
	if err != nil {
		return "", fmt.Errorf("evaluate: %v", err)
	}

	val, ok := out.(float64)
	if !ok {
		return "", fmt.Errorf("expression must be numeric")
	}

	if val == math.Trunc(val) {
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	}
	return strconv.FormatFloat(val, 'f', 6, 64), nil
}

// runDatetime supports current time, calendar-day addition, and days until the
// next January 1. Operations use the requested IANA timezone, defaulting to
// Asia/Shanghai; diff is not a general comparison of two supplied timestamps.
func runDatetime(_ context.Context, args map[string]any, _ string) (string, error) {
	operation, _ := args["operation"].(string)
	tz := "Asia/Shanghai"
	if s, ok := args["tz"].(string); ok && s != "" {
		tz = s
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("bad timezone: %v", err)
	}
	now := time.Now().In(loc)

	switch operation {
	case "now":
		return now.Format(time.RFC3339), nil
	case "add_days":
		days := 0
		switch v := args["days"].(type) {
		case float64:
			days = int(v)
		case int:
			days = v
		}
		// Calendar arithmetic retains local wall-clock semantics across timezone changes.
		return now.AddDate(0, 0, days).Format(time.RFC3339), nil
	case "diff":
		yearEnd := time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, loc)
		return fmt.Sprintf("%d days until %d-01-01",
			int(yearEnd.Sub(now).Hours()/24), now.Year()+1), nil
	default:
		return "", fmt.Errorf("operation must be now/add_days/diff")
	}
}

// Package logging provides local terminal diagnostics using the standard library.
package logging

import (
	"log/slog"
	"os"
)

func Init(level, format string) {
	var severity slog.Level
	if err := severity.UnmarshalText([]byte(level)); err != nil {
		severity = slog.LevelInfo
	}
	options := &slog.HandlerOptions{Level: severity}
	var handler slog.Handler = slog.NewTextHandler(os.Stdout, options)
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}
	slog.SetDefault(slog.New(handler))
}

func LogInfo(msg string, kv ...string)  { slog.Info(msg, fields(kv)...) }
func LogWarn(msg string, kv ...string)  { slog.Warn(msg, fields(kv)...) }
func LogError(msg string, kv ...string) { slog.Error(msg, fields(kv)...) }

func fields(kv []string) []any {
	args := make([]any, 0, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		args = append(args, kv[i], kv[i+1])
	}
	return args
}

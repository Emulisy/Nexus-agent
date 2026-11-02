package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

// FileOpsConfig supplies the filesystem root and maximum bytes returned by reads.
// MaxSize truncates read output; save does not apply that size limit.
type FileOpsConfig struct {
	BaseDir string
	MaxSize int
}

// RegisterFiles captures the file configuration for the file_ops handler.
func RegisterFiles(r Registry, cfg FileOpsConfig) {
	r.Register("file_ops", func(ctx context.Context, args map[string]any, _ string) (string, error) {
		return runFileOps(cfg, args)
	})
}

// runFileOps reads or saves a requested path after cleaning it and checking a
// lexical prefix against BaseDir. This check does not resolve symlinks or establish
// a filesystem sandbox; deployment permissions still determine actual file access.
func runFileOps(cfg FileOpsConfig, args map[string]any) (string, error) {
	operation, _ := args["operation"].(string)
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Normalize before the configured-prefix check. The check is string-based,
	// so it should not be interpreted as a canonical-path containment guarantee.
	clean := filepath.Clean(filepath.Join(cfg.BaseDir, path))
	if !strings.HasPrefix(clean, filepath.Clean(cfg.BaseDir)) {
		return "", fmt.Errorf("path escapes authorized directory")
	}

	switch operation {
	// Bound the model-facing read result without changing the stored file.
	case "read":
		data, err := os.ReadFile(clean)
		if err != nil {
			return "", fmt.Errorf("read file: %v", err)
		}
		if len(data) > cfg.MaxSize {
			data = data[:cfg.MaxSize]
		}
		return string(data), nil
	case "save":
		content, _ := args["content"].(string)
		if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(clean, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write file: %v", err)
		}
		return fmt.Sprintf(`{"saved": %q, "bytes": %d}`, path, len(content)), nil
	default:
		return "", fmt.Errorf("operation must be read/save")
	}
}

// RegisterDBQuery installs SQL querying against the supplied database handle.
// Use a database role with read-only permissions when exposing this handler.
func RegisterDBQuery(r Registry, db *gorm.DB) {
	r.Register("db_query", func(ctx context.Context, args map[string]any, _ string) (string, error) {
		return runDBQuery(ctx, db, args)
	})
}

// dmlRe rejects a list of mutating SQL keywords. It is a lexical filter rather
// than a SQL parser or a substitute for database access control.
var dmlRe = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|truncate|create|grant|revoke|copy)\b`)

// runDBQuery rejects the listed mutating keywords, runs with an eight-second
// context, and returns at most one hundred rows as JSON. It does not independently
// validate that the statement grammar is SELECT-only.
func runDBQuery(ctx context.Context, db *gorm.DB, args map[string]any) (string, error) {
	sqlText, _ := args["sql"].(string)
	if sqlText == "" {
		return "", fmt.Errorf("sql is required")
	}
	if dmlRe.MatchString(sqlText) {
		return "", fmt.Errorf("only read-only SELECT allowed")
	}

	qCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	rows, err := db.WithContext(qCtx).Raw(sqlText).Rows()
	if err != nil {
		return "", fmt.Errorf("query: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}

	// Convert database columns to generic row objects so tools do not depend on
	// application model structs or database-specific binary value representations.
	out := make([]map[string]any, 0, 32)
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			v := values[i]

			// Text-like driver values often arrive as bytes; preserve them as readable
			// JSON strings rather than base64-encoded byte arrays.
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[c] = v
		}
		out = append(out, row)
		if len(out) >= 100 {
			break
		}
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

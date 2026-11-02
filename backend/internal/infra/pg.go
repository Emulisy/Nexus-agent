// Package infra constructs external service clients for the API and worker.
// Startup owns these clients and their configuration; schema migration remains a
// separate command rather than an implicit side effect of opening a SQL connection.
package infra

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/chengpeng-cp/nexus-agent/internal/config"
	"github.com/chengpeng-cp/nexus-agent/internal/logging"
)

// NewPG opens GORM with the configured PostgreSQL DSN, applies SQL pool limits,
// and checks connectivity with a five-second ping. Slow queries use the shared
// structured logger; record-not-found errors are excluded from routine GORM logs.
func NewPG(ctx context.Context, cfg *config.PG) (*gorm.DB, error) {
	logLevel := gormlogger.Warn
	if cfg.DSN == "" {
		return nil, fmt.Errorf("pg dsn is empty")
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger: gormlogger.New(
			gormWriter{},
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  logLevel,
				IgnoreRecordNotFoundError: true,
			},
		),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	logging.LogInfo("postgres connected")
	return db, nil
}

// gormWriter adapts the GORM printf logger to the local console logger.
type gormWriter struct{}

// Printf retains the query diagnostic format and marks the message as GORM output.
func (gormWriter) Printf(format string, args ...any) {
	logging.LogWarn(fmt.Sprintf("[gorm] "+format, args...))
}

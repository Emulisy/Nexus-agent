// Command migrate applies versioned SQL migrations and initializes admin credentials.
// Schema changes are explicit deployment operations rather than part of API
// startup. Upgrades tolerate an already-current schema; a positive -down value
// rolls back that many versions and skips account initialization.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"golang.org/x/crypto/bcrypt"

	"github.com/Emulisy/SupaAgent/internal/config"
	"github.com/Emulisy/SupaAgent/internal/infra"
)

// main parses the rollback count and reports migration failures with a nonzero exit status.
func main() {
	down := flag.Int("down", 0, "Roll back N versions (default 0 applies upgrades only)")
	flag.Parse()

	if err := run(*down); err != nil {
		fmt.Fprintf(os.Stderr, "migrate failed: %v\n", err)
		os.Exit(1)
	}
}

// run loads the shared PostgreSQL configuration and opens the file migration source.
// Successful upgrades, including a no-change result, are followed by admin
// initialization. The migrator closes its source and database handles on return.
func run(down int) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	m, err := migrate.New("file://migrations", cfg.PG.DSN)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if down > 0 {
		if err := m.Steps(-down); err != nil {
			return fmt.Errorf("step down %d: %w", down, err)
		}
		fmt.Printf("rolled back %d version(s)\n", down)
		return nil
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("up: %w", err)
	}
	fmt.Println("migrations applied")

	if err := seedAdmin(cfg.PG.DSN); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	return nil
}

// seedAdmin inserts an administrator only when the users table is empty.
// For an existing installation, it rotates the legacy admin/admin123 password
// while leaving other accounts unchanged. Password hashing occurs here rather
// than in SQL, using a supplied or cryptographically generated bootstrap secret.
func seedAdmin(dsn string) error {
	db, err := infra.NewPG(context.Background(), &config.PG{DSN: dsn, MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	var userCount int64
	if err := db.WithContext(context.Background()).Table("users").Count(&userCount).Error; err != nil {
		return err
	}
	if userCount > 0 {
		var legacyAdmin struct {
			ID           int64
			PasswordHash string `gorm:"column:password_hash"`
		}
		if err := db.WithContext(context.Background()).Table("users").Select("id, password_hash").Where("username = ?", "admin").Take(&legacyAdmin).Error; err != nil {
			fmt.Println("users table not empty, skip seed")
			return nil
		}
		if bcrypt.CompareHashAndPassword([]byte(legacyAdmin.PasswordHash), []byte("admin123")) != nil {
			fmt.Println("users table not empty, skip seed")
			return nil
		}

		password, generated, err := bootstrapPassword()
		if err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		res := db.WithContext(context.Background()).Exec("UPDATE users SET password_hash = ?, updated_at = NOW() WHERE id = ?", string(hash), legacyAdmin.ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("legacy admin password changed concurrently; rerun migration")
		}
		printBootstrapCredential(password, generated, "rotated legacy admin password")
		return nil
	}

	password, generatedPassword, err := bootstrapPassword()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// Insert only while the users table is empty.
	res := db.WithContext(context.Background()).Exec(`
		INSERT INTO users (username, email, password_hash, role, status)
		SELECT 'admin', 'admin@nexus.local', ?, 'admin', 1
		WHERE NOT EXISTS (SELECT 1 FROM users)`, string(hash))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		printBootstrapCredential(password, generatedPassword, "seeded admin user")
	} else {
		fmt.Println("users table not empty, skip seed")
	}
	return nil
}

// bootstrapPassword reads NX_SEED_ADMIN_PASSWORD or generates 24 random bytes
// encoded as URL-safe base64. Both paths enforce a minimum of 16 characters;
// the generated flag tells the caller whether the password must be printed.
func bootstrapPassword() (password string, generated bool, err error) {
	password = os.Getenv("NX_SEED_ADMIN_PASSWORD")
	generated = password == ""
	if generated {
		raw := make([]byte, 24)
		if _, err = rand.Read(raw); err != nil {
			return "", false, fmt.Errorf("generate bootstrap password: %w", err)
		}
		password = base64.RawURLEncoding.EncodeToString(raw)
	}
	if len(password) < 16 {
		return "", false, fmt.Errorf("NX_SEED_ADMIN_PASSWORD must contain at least 16 characters")
	}
	return password, generated, nil
}

// printBootstrapCredential prints generated credentials once for the operator.
// An explicitly configured password is never echoed; the message points back to
// the environment variable used to supply it.
func printBootstrapCredential(password string, generated bool, action string) {
	if generated {
		fmt.Printf("%s: admin / %s (save this password now and change it after your first login)\n", action, password)
	} else {
		fmt.Printf("%s: admin (sign in with the password set by NX_SEED_ADMIN_PASSWORD)\n", action)
	}
}

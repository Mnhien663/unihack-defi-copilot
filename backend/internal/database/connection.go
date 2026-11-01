package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

const databasePingTimeout = 5 * time.Second

type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		Host:     strings.TrimSpace(os.Getenv("DB_HOST")),
		Port:     strings.TrimSpace(os.Getenv("DB_PORT")),
		User:     strings.TrimSpace(os.Getenv("DB_USER")),
		Password: os.Getenv("DB_PASSWORD"),
		Name:     strings.TrimSpace(os.Getenv("DB_NAME")),
		SSLMode:  strings.TrimSpace(os.Getenv("DB_SSLMODE")),
	}

	if config.Host == "" {
		config.Host = "localhost"
	}

	if config.Port == "" {
		config.Port = "5432"
	}

	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}

	if config.User == "" {
		return Config{}, fmt.Errorf("DB_USER is required")
	}

	if config.Name == "" {
		return Config{}, fmt.Errorf("DB_NAME is required")
	}

	return config, nil
}

func Connect(ctx context.Context, config Config) (*sql.DB, error) {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(config.User, config.Password),
		Host:   config.Host + ":" + config.Port,
		Path:   config.Name,
	}

	query := connectionURL.Query()
	query.Set("sslmode", config.SSLMode)
	connectionURL.RawQuery = query.Encode()

	db, err := sql.Open("postgres", connectionURL.String())
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, databasePingTimeout)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		db.Close()

		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return db, nil
}

func ConnectFromEnv(ctx context.Context) (*sql.DB, error) {
	config, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return Connect(ctx, config)
}

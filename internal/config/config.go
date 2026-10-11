package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	Port        string
	Env         string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	var missingVars []string

	dbURL, ok := os.LookupEnv("DATABASE_URL")
	if !ok || strings.TrimSpace(dbURL) == "" {
		missingVars = append(missingVars, "DATABASE_URL")
	}

	port, ok := os.LookupEnv("PORT")

	if !ok || strings.TrimSpace(port) == "" {
		port = "8080"
	}

	env, ok := os.LookupEnv("ENV")

	if !ok || strings.TrimSpace(env) == "" {
		env = "development"
	}

	if len(missingVars) > 0 {
		return nil, fmt.Errorf("missing required variable(s): %s", strings.Join(missingVars, ", "))
	}

	return &Config{
		DatabaseURL: dbURL,
		Port:        port,
		Env:         env,
	}, nil

}

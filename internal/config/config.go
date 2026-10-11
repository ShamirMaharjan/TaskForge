package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	Port        int
	Env         string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	var missingVars []string

	// 1. Database URL check
	dbURL, ok := os.LookupEnv("DATABASE_URL")
	if !ok || strings.TrimSpace(dbURL) == "" {
		missingVars = append(missingVars, "DATABASE_URL is required and cannot be empty.")
	}

	dbURL = strings.TrimSpace(dbURL)

	// 2. Port validation
	portStr, ok := os.LookupEnv("PORT")

	if !ok || strings.TrimSpace(portStr) == "" {
		portStr = "8080"
	}

	portStr = strings.TrimSpace(portStr)

	portNum, err := strconv.Atoi(portStr)

	if err != nil || portNum < 1 || portNum > 65535 {
		missingVars = append(missingVars, fmt.Sprintf("PORT must be an integer between 1 and 65535 (got: %q)", portStr))
	}

	// 3. Environment check
	envStr, ok := os.LookupEnv("ENV")

	if !ok || strings.TrimSpace(envStr) == "" {
		envStr = "development"
	}

	envStr = strings.TrimSpace(envStr)

	if len(missingVars) > 0 {
		return nil, fmt.Errorf("missing required variable(s): %s", strings.Join(missingVars, ", "))
	}

	return &Config{
		DatabaseURL: dbURL,
		Port:        portNum,
		Env:         envStr,
	}, nil

}

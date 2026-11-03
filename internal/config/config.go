package config

import (
	"os"
)

type Config struct {
	Port      string
	DBConnStr string
	RedisAddr string
}

func LoadConfig() *Config {
    port := getEnv("PORT", "8080")

    // Use DATABASE_URL if set (e.g., from Supabase on Render), otherwise build from individual vars
    dbConnStr := os.Getenv("DATABASE_URL")
    if dbConnStr == "" {
        dbHost := getEnv("DB_HOST", "localhost")
        dbPort := getEnv("DB_PORT", "5432")
        dbUser := getEnv("DB_USER", "postgres")
        dbPassword := getEnv("DB_PASSWORD", "secretpassword")
        dbName := getEnv("DB_NAME", "brevity")
        dbConnStr = "postgres://" + dbUser + ":" + dbPassword + "@" + dbHost + ":" + dbPort + "/" + dbName + "?sslmode=disable"
    }
	log.Printf("DEBUG: Connecting to Database URL starting with: %s", maskConnStr(dbConnStr))
    redisAddr := getEnv("REDIS_ADDR", "localhost:6379")

    return &Config{
        Port:      port,
        DBConnStr: dbConnStr,
        RedisAddr: redisAddr,
    }
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func maskConnStr(s string) string {
    if len(s) > 20 {
        return s[:15] + "..." + s[len(s)-10:]
    }
    return "***"
}

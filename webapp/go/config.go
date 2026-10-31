package isureal

import (
	"net"
	"net/url"
	"os"
)

type Config struct {
	Region     string
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	LogLevel   string
	Node       string
	ImageDir   string
}

func ConfigFromEnv() *Config {
	region := envOrDefault("ISUREAL_REGION", "asia")
	return &Config{
		Region: region,
		// サーバーの名前。asia2 はリージョンが asia なので、別の名前を持つ
		Node:       envOrDefault("ISUREAL_NODE", region),
		DBHost:     envOrDefault("ISUREAL_DB_HOST", "127.0.0.1"),
		DBPort:     envOrDefault("ISUREAL_DB_PORT", "5432"),
		DBUser:     envOrDefault("ISUREAL_DB_USER", "isureal"),
		DBPassword: envOrDefault("ISUREAL_DB_PASSWORD", "isureal"),
		DBName:     envOrDefault("ISUREAL_DB_NAME", "isureal"),
		// アクションログは CPU を使うので既定で切る。不正アクセスの判定はログに依存しない
		LogLevel: envOrDefault("ISUREAL_LOG_LEVEL", "off"),
		ImageDir: envOrDefault("ISUREAL_IMAGE_DIR", "/home/isucon/image-cache"),
	}
}

func (c *Config) DatabaseDSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   net.JoinHostPort(c.DBHost, c.DBPort),
		Path:   "/" + c.DBName,
		// DB はチーム内の private IP でしか使わない。TLS の CPU を処理に回す
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

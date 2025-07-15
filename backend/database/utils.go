package database

import (
	"os"
)

// getEnv obtém uma variável de ambiente ou retorna o valor padrão
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
} 
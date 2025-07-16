package database

// NewDatabase cria uma nova instância do banco de dados baseado na variável de ambiente
func NewDatabase() Database {
	dbType := getEnv("DB_TYPE", "postgresql") // Default para SQLite (mais leve)

	switch dbType {
	case "postgres", "postgresql":
		return NewPostgresDB()
	default:
		return NewPostgresDB()
	}
}

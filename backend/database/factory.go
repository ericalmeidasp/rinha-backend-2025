package database

// NewDatabase cria uma nova instância do banco de dados baseado na variável de ambiente
func NewDatabase() Database {
	dbType := getEnv("DB_TYPE", "sqlite") // Default para SQLite (mais leve)

	switch dbType {
	case "postgres", "postgresql":
		return NewPostgresDB()
	case "sqlite", "sqlite3":
		return NewSQLiteDB()
	default:
		// Default para SQLite se não especificado
		return NewSQLiteDB()
	}
}

package database

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"time"

	_ "github.com/lib/pq"
)

// PostgresDB implementa a interface Database para PostgreSQL
type PostgresDB struct {
	db         *sql.DB
	insertStmt *sql.Stmt
}

// NewPostgresDB cria uma nova instância do PostgreSQL
func NewPostgresDB() *PostgresDB {
	return &PostgresDB{}
}

// Connect conecta ao PostgreSQL
func (p *PostgresDB) Connect() error {
	// Usar variáveis de ambiente para configuração
	dbHost := getEnv("DB_HOST", "postgres")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "rinha_backend")

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=2",
		dbHost, dbPort, dbUser, dbPassword, dbName,
	)

	var err error
	p.db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("error opening database: %w", err)
	}

	// Configurar pool de conexões
	p.db.SetMaxOpenConns(8) // Máximo de conexões abertas
	p.db.SetMaxIdleConns(4) // Máximo de conexões ociosas
	// p.db.SetConnMaxLifetime(5 * time.Minute) // Tempo máximo de vida da conexão
	// p.db.SetConnMaxIdleTime(3 * time.Minute) // Tempo máximo ocioso

	// Testar conexão
	if err := p.db.Ping(); err != nil {
		return fmt.Errorf("error connecting to database: %w", err)
	}

	// Preparar statement para inserção
	p.insertStmt, err = p.db.Prepare(`
		INSERT INTO payments (correlation_id, processor, amount, requested_at) 
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return fmt.Errorf("error preparing insert statement: %w", err)
	}

	log.Println("PostgreSQL database connected successfully")
	return nil
}

// Close fecha a conexão com o PostgreSQL
func (p *PostgresDB) Close() error {
	if p.insertStmt != nil {
		p.insertStmt.Close()
	}
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// AddPayment adiciona um pagamento ao PostgreSQL em Batch
func (p *PostgresDB) AddPaymentsBatch(payments []Payment) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction: %w", err)
	}

	stmt := tx.Stmt(p.insertStmt)

	for _, payment := range payments {
		_, err := stmt.Exec(
			payment.CorrelationID,
			payment.Processor,
			int64(math.Round(payment.Amount*100)),
			payment.RequestedAt,
		)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("error executing batch insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing batch insert: %w", err)
	}

	return nil
}

// GetSummary retorna o resumo dos pagamentos do PostgreSQL
func (p *PostgresDB) GetSummary(from, to *time.Time) (Summary, error) {
	args := []interface{}{}
	where := ""

	if from != nil && to != nil {
		where = "WHERE requested_at BETWEEN $1 AND $2"
		args = append(args, from, to)
	} else if from != nil {
		where = "WHERE requested_at >= $1"
		args = append(args, from)
	} else if to != nil {
		where = "WHERE requested_at <= $1"
		args = append(args, to)
	}

	query := fmt.Sprintf(`
		SELECT processor, 
		       COUNT(*) as total_requests, 
		       SUM(amount) as total_amount
		FROM payments
		%s
		GROUP BY processor
	`, where)

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return Summary{}, fmt.Errorf("error querying payments: %w", err)
	}
	defer rows.Close()

	var (
		defaultAmount  float64
		defaultCount   int
		fallbackAmount float64
		fallbackCount  int
	)

	for rows.Next() {
		var processor string
		var totalRequests int
		var totalAmount int64

		err := rows.Scan(&processor, &totalRequests, &totalAmount)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		amount := float64(totalAmount) / 100.0

		switch processor {
		case "default":
			defaultAmount += amount
			defaultCount = totalRequests
		case "fallback":
			fallbackAmount += amount
			fallbackCount = totalRequests
		}
	}

	return Summary{
		Default: ProcessorSummary{
			TotalRequests: defaultCount,
			TotalAmount:   defaultAmount,
		},
		Fallback: ProcessorSummary{
			TotalRequests: fallbackCount,
			TotalAmount:   fallbackAmount,
		},
	}, nil
}

// Ping testa a conectividade com o PostgreSQL
func (p *PostgresDB) Ping() error {
	return p.db.Ping()
}

// PurgePayments remove todos os pagamentos do PostgreSQL
func (p *PostgresDB) PurgePayments() error {
	_, err := p.db.Exec("TRUNCATE payments")
	if err != nil {
		return fmt.Errorf("error purging payments: %w", err)
	}
	return nil
}

package database

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"math/big"
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
	p.db.SetMaxOpenConns(10) // Máximo de conexões abertas
	p.db.SetMaxIdleConns(5)  // Máximo de conexões ociosas
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

// AddPayment adiciona um pagamento ao PostgreSQL
func (p *PostgresDB) AddPayment(payment Payment) error {
	_, err := p.insertStmt.Exec(
		payment.CorrelationID,
		payment.Processor,
		payment.Amount,
		payment.RequestedAt,
	)
	if err != nil {
		return fmt.Errorf("error saving payment: %w", err)
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
	var query string
	var args []interface{}

	baseQuery := `
		SELECT processor, 
		       COUNT(*) as total_requests, 
		       SUM(amount) as total_amount
		FROM payments
	`

	if from != nil && to != nil {
		query = baseQuery + " WHERE requested_at BETWEEN $1 AND $2 GROUP BY processor"
		args = []interface{}{from, to}
	} else if from != nil {
		query = baseQuery + " WHERE requested_at >= $1 GROUP BY processor"
		args = []interface{}{from}
	} else if to != nil {
		query = baseQuery + " WHERE requested_at <= $1 GROUP BY processor"
		args = []interface{}{to}
	} else {
		query = baseQuery + " GROUP BY processor"
	}

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return Summary{}, fmt.Errorf("error querying payments: %w", err)
	}
	defer rows.Close()

	// Usar math/big para somas precisas
	defaultSum := new(big.Float)
	fallbackSum := new(big.Float)
	defaultCount := 0
	fallbackCount := 0

	for rows.Next() {
		var processor string
		var totalRequests int
		var totalAmount int64 // Usar int64 para evitar problemas de precisão com float64

		err := rows.Scan(&processor, &totalRequests, &totalAmount)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		// Converter para big.Float para precisão
		amountBig := new(big.Float).SetFloat64(float64(totalAmount) / 100.0)

		if processor == "default" {
			defaultSum.Add(defaultSum, amountBig)
			defaultCount = totalRequests
		} else if processor == "fallback" {
			fallbackSum.Add(fallbackSum, amountBig)
			fallbackCount = totalRequests
		}
	}

	// Converter de volta para float64 para o JSON
	defaultAmount, _ := defaultSum.Float64()
	fallbackAmount, _ := fallbackSum.Float64()

	result := Summary{
		Default: ProcessorSummary{
			TotalRequests: defaultCount,
			TotalAmount:   defaultAmount,
		},
		Fallback: ProcessorSummary{
			TotalRequests: fallbackCount,
			TotalAmount:   fallbackAmount,
		},
	}

	return result, nil
}

// Ping testa a conectividade com o PostgreSQL
func (p *PostgresDB) Ping() error {
	return p.db.Ping()
}

// PurgePayments remove todos os pagamentos do PostgreSQL
func (p *PostgresDB) PurgePayments() error {
	_, err := p.db.Exec("DELETE FROM payments")
	if err != nil {
		return fmt.Errorf("error purging payments: %w", err)
	}
	return nil
}

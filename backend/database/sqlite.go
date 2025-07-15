package database

import (
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteDB implementa a interface Database para SQLite
type SQLiteDB struct {
	db         *sql.DB
	insertStmt *sql.Stmt
}

// NewSQLiteDB cria uma nova instância do SQLite
func NewSQLiteDB() *SQLiteDB {
	return &SQLiteDB{}
}

// Connect conecta ao SQLite
func (s *SQLiteDB) Connect() error {
	// SQLite é mais leve que PostgreSQL
	dbPath := getEnv("DB_PATH", "/tmp/payments.db")

	var err error
	s.db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("error opening database: %w", err)
	}

	// Configurar pool de conexões (SQLite é mais simples)
	s.db.SetMaxOpenConns(1) // SQLite é single-writer
	s.db.SetMaxIdleConns(1)
	s.db.SetConnMaxLifetime(0) // Sem limite de tempo

	// Criar tabela se não existir
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS payments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		correlation_id TEXT NOT NULL,
		processor TEXT NOT NULL,
		amount REAL NOT NULL,
		requested_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_payments_processor ON payments(processor);
	CREATE INDEX IF NOT EXISTS idx_payments_requested_at ON payments(requested_at);
	`

	_, err = s.db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}

	// Preparar statement para inserção
	s.insertStmt, err = s.db.Prepare(`
		INSERT INTO payments (correlation_id, processor, amount, requested_at) 
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("error preparing insert statement: %w", err)
	}

	log.Println("SQLite database connected successfully")
	return nil
}

// Close fecha a conexão com o SQLite
func (s *SQLiteDB) Close() error {
	if s.insertStmt != nil {
		s.insertStmt.Close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// AddPayment adiciona um pagamento ao SQLite
func (s *SQLiteDB) AddPayment(payment Payment) error {
	_, err := s.insertStmt.Exec(
		payment.CorrelationID,
		payment.Processor,
		payment.Amount,
		payment.RequestedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("error saving payment: %w", err)
	}
	return nil
}

// AddPayment adiciona um pagamento ao SQLite TODO rever
func (s *SQLiteDB) AddPaymentsBatch(payments []Payment) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("error beginning transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO payments (correlation_id, processor, amount, requested_at) 
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("error preparing batch insert: %w", err)
	}
	defer stmt.Close()

	for _, payment := range payments {
		_, err := stmt.Exec(
			payment.CorrelationID,
			payment.Processor,
			payment.Amount,
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

// GetSummary retorna o resumo dos pagamentos do SQLite
func (s *SQLiteDB) GetSummary(from, to *time.Time) (Summary, error) {
	var query string
	var args []interface{}

	baseQuery := `
		SELECT processor, 
		       COUNT(*) as total_requests, 
		       SUM(amount) as total_amount
		FROM payments
	`

	if from != nil && to != nil {
		query = baseQuery + " WHERE requested_at BETWEEN ? AND ? GROUP BY processor"
		args = []interface{}{from.Format(time.RFC3339), to.Format(time.RFC3339)}
	} else if from != nil {
		query = baseQuery + " WHERE requested_at >= ? GROUP BY processor"
		args = []interface{}{from.Format(time.RFC3339)}
	} else if to != nil {
		query = baseQuery + " WHERE requested_at <= ? GROUP BY processor"
		args = []interface{}{to.Format(time.RFC3339)}
	} else {
		query = baseQuery + " GROUP BY processor"
	}

	rows, err := s.db.Query(query, args...)
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
		var totalAmount float64

		err := rows.Scan(&processor, &totalRequests, &totalAmount)
		if err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		// Converter para big.Float para precisão
		amountBig := new(big.Float).SetFloat64(totalAmount)

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

// Ping testa a conectividade com o SQLite
func (s *SQLiteDB) Ping() error {
	return s.db.Ping()
}

// PurgePayments remove todos os pagamentos do SQLite
func (s *SQLiteDB) PurgePayments() error {
	_, err := s.db.Exec("DELETE FROM payments")
	if err != nil {
		return fmt.Errorf("error purging payments: %w", err)
	}
	return nil
}

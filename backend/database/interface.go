package database

import (
	"time"
)

// Payment representa um pagamento processado
type Payment struct {
	CorrelationID string
	Processor     string
	Amount        float64
	RequestedAt   time.Time
}

// Summary representa o resumo dos pagamentos
type Summary struct {
	Default  ProcessorSummary `json:"default"`
	Fallback ProcessorSummary `json:"fallback"`
}

// ProcessorSummary representa o resumo de um processador
// IMPORTANTE: Os valores devem ser precisos (sem problemas de float64 como 0.1+0.2)
type ProcessorSummary struct {
	TotalRequests int     `json:"totalRequests"`
	TotalAmount   float64 `json:"totalAmount"`
}

// Database interface para abstrair operações de banco
type Database interface {
	// Connect conecta ao banco de dados
	Connect() error

	// Close fecha a conexão com o banco
	Close() error

	// AddPayment adiciona um pagamento ao banco em batch
	AddPaymentsBatch(payments []Payment) error

	// GetSummary retorna o resumo dos pagamentos no período especificado
	// IMPORTANTE: Deve garantir precisão decimal nas somas
	GetSummary(from, to *time.Time) (Summary, error)

	// Ping testa a conectividade com o banco
	Ping() error

	// PurgePayments remove todos os pagamentos da base
	PurgePayments() error
}

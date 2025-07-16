package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"rinha-backend/database"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	defaultProcessorURL    = "http://payment-processor-default:8080/payments"
	fallbackProcessorURL   = "http://payment-processor-fallback:8080/payments"
	healthCheckDefaultURL  = "http://payment-processor-default:8080/payments/service-health"
	healthCheckFallbackURL = "http://payment-processor-fallback:8080/payments/service-health"
)

var db database.Database
var paymentBufferMutex sync.Mutex
var paymentBuffer []database.Payment

// PaymentRequest representa o corpo esperado no POST /payments
type PaymentRequest struct {
	CorrelationID string  `json:"correlationId" binding:"required"`
	Amount        float64 `json:"amount" binding:"required"`
}

type processorResponse struct {
	Message string `json:"message"`
}

type healthCheckResult struct {
	Failing         bool `json:"failing"`
	MinResponseTime int  `json:"minResponseTime"`
}

type healthCache struct {
	Result    healthCheckResult
	CheckedAt time.Time
	Err       error
}

var (
	healthCacheMap = map[string]*healthCache{
		"default":  {},
		"fallback": {},
	}
	healthCacheMutex sync.Mutex
)

// Estrutura para fila assíncrona de pagamentos
type PaymentJob struct {
	Req         PaymentRequest
	RequestedAt time.Time
}

var paymentQueue chan PaymentJob

func initDB() error {
	log.Println("Initializing database...")

	dbType := os.Getenv("DB_TYPE")
	if dbType == "" {
		dbType = "sqlite"
	}
	log.Printf("Database type: %s", dbType)

	db = database.NewDatabase()

	log.Println("Connecting to database...")

	maxRetries := 10
	var err error

	for i := 1; i <= maxRetries; i++ {
		err = db.Connect()
		if err == nil {
			log.Println("Database initialized successfully")
			return nil
		}

		log.Printf("Tentativa %d de conexão falhou: %v", i, err)
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("failed to connect to database after %d attempts: %w", maxRetries, err)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func addPayment(processor string, cid string, amount float64, requestedAt time.Time) {
	payment := database.Payment{
		CorrelationID: cid,
		Processor:     processor,
		Amount:        amount,
		RequestedAt:   requestedAt,
	}

	if err := db.AddPayment(payment); err != nil {
		log.Printf("Error saving payment to database: %v", err)
	} else {
		log.Printf("Payment saved successfully: processor=%s, amount=%.2f", processor, amount)
	}
}

func addPaymentBatch(processor string, cid string, amount float64, requestedAt time.Time) {
	payment := database.Payment{
		CorrelationID: cid,
		Processor:     processor,
		Amount:        amount,
		RequestedAt:   requestedAt,
	}

	var batchToFlush []database.Payment

	paymentBufferMutex.Lock()
	paymentBuffer = append(paymentBuffer, payment)

	if len(paymentBuffer) >= 100 {
		// Faz uma cópia para não segurar o lock durante o insert
		batchToFlush = make([]database.Payment, len(paymentBuffer))
		copy(batchToFlush, paymentBuffer)
		paymentBuffer = paymentBuffer[:0]
	}
	paymentBufferMutex.Unlock()

	if batchToFlush != nil {
		err := db.AddPaymentsBatch(batchToFlush)
		if err != nil {
			log.Printf("erro no batch insert: %v", err)
		}
	}
}

func getSummary(from, to *time.Time) map[string]map[string]interface{} {
	summary, err := db.GetSummary(from, to)
	if err != nil {
		log.Printf("Error getting summary: %v", err)
		return map[string]map[string]interface{}{
			"default":  {"totalRequests": 0, "totalAmount": 0.0},
			"fallback": {"totalRequests": 0, "totalAmount": 0.0},
		}
	}

	return map[string]map[string]interface{}{
		"default": map[string]interface{}{
			"totalRequests": summary.Default.TotalRequests,
			"totalAmount":   summary.Default.TotalAmount,
		},
		"fallback": map[string]interface{}{
			"totalRequests": summary.Fallback.TotalRequests,
			"totalAmount":   summary.Fallback.TotalAmount,
		},
	}
}

func sendToProcessor(url string, req PaymentRequest, requestedTime time.Time) error {
	body := map[string]interface{}{
		"correlationId": req.CorrelationID,
		"amount":        req.Amount,
		"requestedAt":   requestedTime.Format(time.RFC3339Nano),
	}
	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		fmt.Println("Error sending to processor", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		fmt.Println("Error sending to processor status code", err)
		return io.ErrUnexpectedEOF // sinaliza erro para fallback
	}
	return nil
}

func getHealth(processor string, url string) (healthCheckResult, error) {
	healthCacheMutex.Lock()
	cache := healthCacheMap[processor]
	now := time.Now()
	if cache != nil && now.Sub(cache.CheckedAt) < 5*time.Second {
		res, err := cache.Result, cache.Err
		healthCacheMutex.Unlock()
		return res, err
	}
	healthCacheMutex.Unlock()

	resp, err := http.Get(url)
	if err != nil {
		healthCacheMutex.Lock()
		cache.Result = healthCheckResult{}
		cache.Err = err
		cache.CheckedAt = now
		healthCacheMutex.Unlock()
		return healthCheckResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return healthCheckResult{}, errors.New("rate limited on health-check")
	}
	var result healthCheckResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return healthCheckResult{}, err
	}
	healthCacheMutex.Lock()
	cache.Result = result
	cache.Err = nil
	cache.CheckedAt = now
	healthCacheMutex.Unlock()
	return result, nil
}

func startPaymentWorkers(n int) {
	for i := 0; i < n; i++ {
		go func() {
			for job := range paymentQueue {
				processPaymentJob(job)
			}
		}()
	}
}

func processPaymentJob(job PaymentJob) {

	// Health-check do default
	//defaultHealth, err := getHealth("default", healthCheckDefaultURL)

	//fallbackHealth, err2 := getHealth("fallback", healthCheckFallbackURL)
	var err, err2 error

	healthCacheMutex.Lock()
	defaultHealth := healthCacheMap["default"].Result
	fallbackHealth := healthCacheMap["fallback"].Result
	healthCacheMutex.Unlock()

	if !defaultHealth.Failing && !fallbackHealth.Failing {

		if defaultHealth.MinResponseTime <= fallbackHealth.MinResponseTime {
			err = sendToProcessor(defaultProcessorURL, job.Req, job.RequestedAt)
			if err == nil {
				addPaymentBatch("default", job.Req.CorrelationID, job.Req.Amount, job.RequestedAt)
				return
			}
		}
		err2 = sendToProcessor(fallbackProcessorURL, job.Req, job.RequestedAt)
		if err2 == nil {
			addPaymentBatch("fallback", job.Req.CorrelationID, job.Req.Amount, job.RequestedAt)
			return
		}
	}

	if !defaultHealth.Failing {

		err = sendToProcessor(defaultProcessorURL, job.Req, job.RequestedAt)
		if err == nil {
			addPaymentBatch("default", job.Req.CorrelationID, job.Req.Amount, job.RequestedAt)
			return
		}
	}

	if !defaultHealth.Failing {

		err2 = sendToProcessor(fallbackProcessorURL, job.Req, job.RequestedAt)
		if err2 == nil {
			addPaymentBatch("fallback", job.Req.CorrelationID, job.Req.Amount, job.RequestedAt)
			return
		}
	}

	paymentQueue <- job // Re-enqueue the job if both processors fail

	log.Printf("Payment failed: correlationId=%s, re-enqueue job", job.Req.CorrelationID)
}

func startPaymentFlushLoop() {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			var batchToFlush []database.Payment

			paymentBufferMutex.Lock()
			if len(paymentBuffer) > 0 {
				batchToFlush = make([]database.Payment, len(paymentBuffer))
				copy(batchToFlush, paymentBuffer)
				paymentBuffer = paymentBuffer[:0]
			}
			paymentBufferMutex.Unlock()

			if batchToFlush != nil {
				err := db.AddPaymentsBatch(batchToFlush)
				if err != nil {
					log.Printf("erro no flush automático: %v", err)
				}
			}
		}
	}()
}

func startHealthCheckLoop() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			getHealth("default", healthCheckDefaultURL)
			getHealth("fallback", healthCheckFallbackURL)
		}
	}()
}

func main() {
	if err := initDB(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	paymentQueue = make(chan PaymentJob, 10000)
	startPaymentWorkers(40)
	// Inicia goroutine de flush periódico
	startPaymentFlushLoop()
	startHealthCheckLoop()

	r := gin.Default()

	r.POST("/payments", func(c *gin.Context) {
		var req PaymentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if _, err := uuid.Parse(req.CorrelationID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid correlationId"})
			return
		}
		if req.Amount <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "amount must be positive"})
			return
		}

		requestedAt := time.Now().UTC()
		paymentQueue <- PaymentJob{Req: req, RequestedAt: requestedAt}
		c.Status(http.StatusAccepted)
	})

	r.POST("/purge-payments", func(c *gin.Context) {
		err := db.PurgePayments()
		if err != nil {
			log.Printf("Error purging payments: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to purge payments"})
			return
		}
		c.Status(http.StatusNoContent)
	})

	r.GET("/payments-summary", func(c *gin.Context) {
		var fromPtr, toPtr *time.Time
		from := c.Query("from")
		to := c.Query("to")
		if from != "" {
			if t, err := time.Parse(time.RFC3339, from); err == nil {
				fromPtr = &t
			}
		}
		if to != "" {
			if t, err := time.Parse(time.RFC3339, to); err == nil {
				toPtr = &t
			}
		}
		c.JSON(http.StatusOK, getSummary(fromPtr, toPtr))
	})

	r.Run(":8080")
}

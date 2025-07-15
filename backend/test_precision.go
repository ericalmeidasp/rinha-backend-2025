package main

import (
	"fmt"
	"math/big"
)

func testPrecision() {
	fmt.Println("=== Teste de Precisão Decimal ===")

	// Simular alguns pagamentos com valores que causam problemas de precisão
	payments := []struct {
		processor string
		amount    float64
	}{
		{"default", 0.1},
		{"default", 0.2},
		{"default", 0.3},
		{"fallback", 0.1},
		{"fallback", 0.2},
		{"fallback", 0.3},
	}

	// Usar math/big para somas precisas
	defaultSum := new(big.Float)
	fallbackSum := new(big.Float)
	defaultCount := 0
	fallbackCount := 0

	for _, payment := range payments {
		amountBig := new(big.Float).SetFloat64(payment.amount)

		if payment.processor == "default" {
			defaultSum.Add(defaultSum, amountBig)
			defaultCount++
		} else if payment.processor == "fallback" {
			fallbackSum.Add(fallbackSum, amountBig)
			fallbackCount++
		}
	}

	// Converter de volta para float64
	defaultAmount, _ := defaultSum.Float64()
	fallbackAmount, _ := fallbackSum.Float64()

	fmt.Printf("Default: %d pagamentos, total: %.2f (esperado: 0.60)\n", defaultCount, defaultAmount)
	fmt.Printf("Fallback: %d pagamentos, total: %.2f (esperado: 0.60)\n", fallbackCount, fallbackAmount)

	// Teste direto com float64 (problema)
	floatSum := 0.1 + 0.2 + 0.3
	fmt.Printf("Float64 direto: %.2f (problema: %t)\n", floatSum, floatSum != 0.6)

	fmt.Println("=== Fim do Teste ===")
}

// func main() {
// 	testPrecision()
// }

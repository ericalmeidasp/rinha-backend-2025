CREATE TABLE IF NOT EXISTS payments (
    id SERIAL PRIMARY KEY,
    correlation_id VARCHAR(36) NOT NULL,
    processor VARCHAR(20) NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    requested_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_processor_requested ON payments(processor, requested_at);
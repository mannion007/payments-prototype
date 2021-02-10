package handler

type Amount struct {
	Currency string `json:"currency"`
	Value    int64  `json:"value"`
}

type Expiry struct {
	Year  string `json:"year"`
	Month string `json:"month"`
}

type Card struct {
	Number string `json:"number"`
	Expiry Expiry `json:"expiry"`
}

type ClaimRequest struct {
	IdempotencyToken string `json:"idempotency_token"`
	PayeeId          string `json:"payee_id"`
	Amount           Amount `json:"amount"`
	Card             Card   `json:"card"`
}

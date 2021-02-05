package payment

// Processor defines the behaviour required of a Payment Service Provider
type Processor interface {
	Process(*Claim) (*Outcome, error)
}

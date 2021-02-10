package processor

import (
	"fmt"

	"github.com/mannion007/payments-prototype/pkg/payment"

	stripe "github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/charge"
	"github.com/stripe/stripe-go/token"
)

type takePaymentResponse struct {
	Reference string `json:"reference"`
	Success   bool   `json:"success"`
}

// StripeProcessor is a Processor which talks to wiremock over http
type StripeProcessor struct{}

//Process will talk to stripe over http to process the Claim, returing an error, if any
func (stripeProc StripeProcessor) Process(c *payment.Claim) (*payment.Outcome, error) {

	// Create card token
	tokenParams := &stripe.TokenParams{
		Card: &stripe.CardParams{
			Number:   stripe.String(c.Payer.Number),
			ExpMonth: stripe.String(c.Payer.ExpiresAt.Month),
			ExpYear:  stripe.String(c.Payer.ExpiresAt.Year),
		},
	}

	t, err := token.New(tokenParams)

	if err != nil {
		return nil, fmt.Errorf("failed to create card token, %s", err.Error())
	}

	d := fmt.Sprintf("deko id: %s payee: %s", c.ID, c.Payee)

	// Create charge object
	chargeParams := &stripe.ChargeParams{
		Amount:      stripe.Int64(int64(c.Amount.Value)),
		Currency:    stripe.String(string(stripe.CurrencyGBP)),
		Description: stripe.String(d),
		Source:      &stripe.SourceParams{Token: &t.ID},
	}
	charge, err := charge.New(chargeParams)

	if err != nil {
		panic(err)
	}

	success := false
	if charge.Status == "succeeded" {
		success = true
	}

	outcome := payment.Outcome{VendorReference: charge.ID, Success: success}

	return &outcome, nil
}

// NewStripeProcessor is a facotry for a StripeProcessor with sensible defaults
func NewStripeProcessor() *StripeProcessor {

	stripe.Key = ""

	return &StripeProcessor{}
}

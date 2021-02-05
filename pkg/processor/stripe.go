package processor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/mannion007/payments-prototype/pkg/payment"
)

const stripeTakePaymentEndpoint = "http://localhost:8080/take-payment"

type takePaymentResponse struct {
	Reference string `json:"reference"`
	Success   bool   `json:"success"`
}

// StripeProcessor is a Processor which talks to wiremock over http
type StripeProcessor struct {
	client              *http.Client
	takePaymentEndpoint string
}

//Process will talk to stripe over http to process the Claim, returing an error, if any
func (stripeProc StripeProcessor) Process(claim *payment.Claim) (*payment.Outcome, error) {
	payload := bufio.NewReader(strings.NewReader(`{"card_number": ` + claim.Payer.Number + `}`))

	req, err := http.NewRequest(http.MethodPost, stripeProc.takePaymentEndpoint, payload)
	if err != nil {
		return nil, err
	}

	resp, err := stripeProc.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to take the payment, got response code %d", resp.StatusCode)
	}

	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var response takePaymentResponse

	json.Unmarshal(contents, &response)

	outcome := payment.Outcome{VendorReference: response.Reference, Success: true}

	return &outcome, nil
}

// NewStripeProcessor is a facotry for a StripeProcessor with sensible defaults
func NewStripeProcessor() *StripeProcessor {

	httpClient := &http.Client{
		Timeout: time.Duration(time.Second * 3),
	}

	proc := StripeProcessor{
		client:              httpClient,
		takePaymentEndpoint: stripeTakePaymentEndpoint,
	}

	return &proc
}

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-amqp/pkg/amqp"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/message/router/plugin"
	"github.com/google/uuid"
)

const (
	maxRetries                = 10
	amqpURI                   = "amqp://guest:guest@localhost:5672/"
	commandTopic              = "payment_commands"
	eventTopic                = "payment_events"
	stripeTakePaymentEndpoint = "http://localhost:8080/take-payment"
)

var (
	logger = watermill.NewStdLogger(false, false)
)

// MonetaryAmount is comprised of a currency i.e. "GBP" and a value in the smallest demonimation i.e. GBP, 100 = £1.00
type MonetaryAmount struct {
	Currency string `json:"currrency"`
	Value    int    `json:"value"`
}

// ExpirationDate is an aggretate of the year and month that a card expires
type ExpirationDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

// Card is a debit or credit card which we can use to process PaymentClaims
type Card struct {
	Number    string         `json:"number"`
	ExpiresAt ExpirationDate `json:"expires_at"`
}

// PaymentClaim is an aggregate of all the details required to take a payment from a card
type PaymentClaim struct {
	Payee  uuid.UUID      `json:"payee"`
	Amount MonetaryAmount `json:"amount"`
	Payer  Card           `json:"payer"`
}

//NewPaymentClaim is a factory for PaymentClaim
func NewPaymentClaim(payee uuid.UUID, amountCurrency string, amountValue int, cardNumber string, expiryYear int, expiryMonth int) (*PaymentClaim, error) {

	// todo: validation guards

	paymentClaim := PaymentClaim{
		Payee: payee,
		Amount: MonetaryAmount{
			Currency: amountCurrency,
			Value:    amountValue,
		},
		Payer: Card{
			Number: cardNumber,
			ExpiresAt: ExpirationDate{
				Year:  expiryYear,
				Month: expiryMonth,
			},
		},
	}

	return &paymentClaim, nil
}

// PaymentProcessor defines the behaviour required of a Payment Service Provider
type PaymentProcessor interface {
	Process(*PaymentClaim) error
}

// StripePaymentProcessor is a PaymentProcessor which talks to wiremock over http
type StripePaymentProcessor struct {
	client              *http.Client
	takePaymentEndpoint string
}

//Process will talk to stripe over http to process the paymentClaim, returing an error, if any
func (stripeProc StripePaymentProcessor) Process(paymentClaim *PaymentClaim) error {
	payload := bufio.NewReader(strings.NewReader(`{"card_number": ` + paymentClaim.Payer.Number + `}`))

	req, err := http.NewRequest(http.MethodPost, stripeProc.takePaymentEndpoint, payload)
	if err != nil {
		return err
	}

	resp, err := stripeProc.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to take the payment, got response code %d", resp.StatusCode)
	}

	return nil
}

// NewStripePaymentProcessor is a facotry for a StripePaymentProcessor with sensible defaults
func NewStripePaymentProcessor() *StripePaymentProcessor {

	httpClient := &http.Client{
		Timeout: time.Duration(time.Second * 3),
	}

	proc := StripePaymentProcessor{
		client:              httpClient,
		takePaymentEndpoint: stripeTakePaymentEndpoint,
	}

	return &proc
}

// TakePaymentHandler is a message handler which takes payments
type TakePaymentHandler struct {
	Processor PaymentProcessor
}

//ProcessPaymentClaim handles messages using a PaymentProcessor, returning a resulting message and an error, if any
func (tph TakePaymentHandler) ProcessPaymentClaim(msg *message.Message) ([]*message.Message, error) {
	log.Println("take payment handler received message", msg.UUID)

	var paymentClaim *PaymentClaim

	err := json.Unmarshal(msg.Payload, &paymentClaim)
	if err != nil {
		panic(err)
	}

	if err := tph.Processor.Process(paymentClaim); err != nil {
		return nil, fmt.Errorf("error when processing command [%s]", err)
	}

	event := message.NewMessage(watermill.NewUUID(), []byte("payment taken successfully!"))
	return message.Messages{event}, nil
}

// NewTakePaymentHandler is a fatory for a TakePaymentHandler
func NewTakePaymentHandler() *TakePaymentHandler {
	processor := NewStripePaymentProcessor()

	handler := TakePaymentHandler{
		Processor: processor,
	}

	return &handler
}

func main() {

	// configure router with middleware
	router, err := message.NewRouter(message.RouterConfig{}, logger)
	if err != nil {
		panic(err)
	}

	router.AddPlugin(plugin.SignalsHandler) // gracefully shutdown wht router

	router.AddMiddleware(
		middleware.CorrelationID, // add and chain correlation id through messages for a given process
		middleware.Retry{
			MaxRetries:      maxRetries,
			InitialInterval: time.Second,
			Multiplier:      2.0,
		}.Middleware, // retry upto 10 times with a backoff duration 1s, 2s, 4s, 8s...
		middleware.Recoverer, // recovers from panics in handlers, enabling the message to be retried
	)

	amqpConfig := amqp.NewDurableQueueConfig(amqpURI)

	// configure subscriber
	subscriber, err := amqp.NewSubscriber(amqpConfig, logger)
	if err != nil {
		panic(err)
	}
	defer subscriber.Close()

	// [DEBUG] print all the events produced
	router.AddNoPublisherHandler(
		"print_outgoing_messages",
		eventTopic,
		subscriber,
		printMessages,
	)

	// configure publisher
	publisher, err := amqp.NewPublisher(amqpConfig, logger)
	if err != nil {
		panic(err)
	}
	defer publisher.Close()

	// instantiate the handler
	takePaymentHandler := NewTakePaymentHandler()

	// start publishing messages
	go publishMessages(publisher)

	// add a handler for taking payments to the router
	router.AddHandler(
		"process_payment_handler",
		commandTopic,
		subscriber,
		eventTopic,
		publisher,
		takePaymentHandler.ProcessPaymentClaim,
	)

	// Run the router
	if err := router.Run(context.Background()); err != nil {
		panic(err)
	}

}

// publishMessages loops forever puttiing commands onto the command bus
func publishMessages(publisher message.Publisher) {

	paymentClaim, err := NewPaymentClaim(uuid.New(), "GBP", 10000, "13656978", 2025, 12)
	if err != nil {
		panic(err)
	}

	payload, err := json.Marshal(paymentClaim)
	if err != nil {
		panic(err)
	}

	for {

		time.Sleep(time.Duration(time.Second))

		msg := message.NewMessage(watermill.NewUUID(), payload)
		middleware.SetCorrelationID(watermill.NewUUID(), msg)

		log.Printf("sending command %s, correlation id: %s\n", msg.UUID, middleware.MessageCorrelationID(msg))

		if err := publisher.Publish(commandTopic, msg); err != nil {
			panic(err)
		}
	}
}

// for debug only, output information about a message
func printMessages(msg *message.Message) error {
	fmt.Printf(
		"\n> Received message: %s\n> %s\n> metadata: %v\n\n",
		msg.UUID, string(msg.Payload), msg.Metadata,
	)
	return nil
}

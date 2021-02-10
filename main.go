package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	stdHttp "net/http"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-amqp/pkg/amqp"
	"github.com/ThreeDotsLabs/watermill-http/pkg/http"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/message/router/plugin"
	"github.com/mannion007/payments-prototype/pkg/handler"
	"github.com/mannion007/payments-prototype/pkg/payment"
	"github.com/mannion007/payments-prototype/pkg/processor"
	"google.golang.org/protobuf/proto"
)

const (
	maxRetries   = 3
	amqpURI      = "amqp://guest:guest@localhost:5672/"
	commandTopic = "payment_commands"
	eventTopic   = "payment_events"
)

var (
	logger   = watermill.NewStdLogger(false, false)
	httpAddr = ":8888"
)

func main() {

	// configure router with middleware
	router, err := message.NewRouter(message.RouterConfig{}, logger)
	if err != nil {
		panic(err)
	}

	amqpConfig := amqp.NewDurableQueueConfig(amqpURI)

	// configure http subscriber (takes http request ad publishes message to bus)
	httpSubscriber, err := http.NewSubscriber(
		httpAddr,
		http.SubscriberConfig{
			UnmarshalMessageFunc: func(topic string, request *stdHttp.Request) (*message.Message, error) {
				b, err := ioutil.ReadAll(request.Body)
				if err != nil {
					return nil, fmt.Errorf("failed to decode http payload, %s", err.Error())
				}

				return message.NewMessage(watermill.NewUUID(), b), nil
			},
		},
		logger,
	)

	if err != nil {
		panic(err)
	}

	// configure message subscriber (takes message from bus and processes it)
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

	// add plugins and middleware
	router.AddPlugin(plugin.SignalsHandler) // gracefully shutdown wht router

	router.AddMiddleware(
		middleware.CorrelationID, // add and chain correlation id through messages for a given process
		middleware.Retry{
			MaxRetries:      maxRetries,
			InitialInterval: time.Second,
			Multiplier:      2.0,
		}.Middleware, // retry upto 10 times with a backoff duration 1s, 2s, 4s, 8s...
		middleware.Recoverer, // recovers from panics in handlers, enabling the message to be retried
		middleware.NewThrottle(1000, time.Second).Middleware, // slow down the processing to 10 messages per second
	)

	// instantiate the handler
	processor := processor.NewStripeProcessor()
	claimPaymentHandler := handler.NewClaimPayment(processor)

	// add a handler for converting web requests to commands
	router.AddHandler(
		"http_to_bus",
		"/pay",
		httpSubscriber,
		commandTopic,
		publisher,
		func(msg *message.Message) ([]*message.Message, error) {

			// unmarshall vlaim request from json (from web request)
			cr := &handler.ClaimRequest{}
			json.Unmarshal(msg.Payload, cr)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal http payload, %s", err.Error())
			}

			// instatiate claim from request
			claim := &payment.Claim{
				ID:     cr.IdempotencyToken,
				Payee:  cr.PayeeId,
				Amount: &payment.Claim_MonetaryAmount{Currency: cr.Amount.Currency, Value: cr.Amount.Value},
				Payer: &payment.Claim_Card{
					Number:    cr.Card.Number,
					ExpiresAt: &payment.Claim_ExpirationDate{Year: cr.Card.Expiry.Year, Month: cr.Card.Expiry.Month},
				},
			}

			// marshall claim to protobuf (for message bus)
			buf, err := proto.Marshal(claim)
			if err != nil {
				panic(err)
			}

			m := message.NewMessage(watermill.NewUUID(), buf)
			middleware.SetCorrelationID(watermill.NewUUID(), msg)

			return []*message.Message{m}, nil

		},
	)

	// add a handler for taking payments to the router
	router.AddHandler(
		"process_payment_handler",
		commandTopic,
		subscriber,
		eventTopic,
		publisher,
		claimPaymentHandler.Process,
	)

	go func() {
		// wait until the router is running then start the webserver in another go routine
		<-router.Running()
		_ = httpSubscriber.StartHTTPServer()
	}()

	// run the router
	if err := router.Run(context.Background()); err != nil {
		panic(err)
	}
}

// [DEBUG] output information about a message
func printMessages(msg *message.Message) error {

	outcome := &payment.Outcome{}

	err := proto.Unmarshal(msg.Payload, outcome)
	if err != nil {
		return fmt.Errorf("failed to unmarshal outcome")
	}

	log.Printf("outcome reached reference: %s, success: %t", outcome.VendorReference, outcome.Success)

	return nil
}

package main

import (
	"context"
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
	"github.com/google/uuid"
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
					return nil, err
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
			// todo - make this interpret the web request and build the command from it (this is all just hard coded)
			payee := uuid.New()

			claim := &payment.Claim{
				Payee:  payee.String(),
				Amount: &payment.Claim_MonetaryAmount{Currency: "GBP", Value: 10000},
				Payer: &payment.Claim_Card{
					Number:    "13656978",
					ExpiresAt: &payment.Claim_ExpirationDate{Year: 2025, Month: 12},
				},
			}

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

	// Run the router
	if err := router.Run(context.Background()); err != nil {
		panic(err)
	}
}

// [DEBUG] output information about a message
func printMessages(msg *message.Message) error {
	log.Printf("receive message %s metadata: %v\n", msg.UUID, msg.Metadata)

	return nil
}

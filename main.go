package main

import (
	"context"
	"log"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-amqp/pkg/amqp"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/ThreeDotsLabs/watermill/message/router/plugin"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/mannion007/payments-prototype/pkg/handler"
	"github.com/mannion007/payments-prototype/pkg/payment"
	"github.com/mannion007/payments-prototype/pkg/processor"
)

const (
	maxRetries   = 3
	amqpURI      = "amqp://guest:guest@localhost:5672/"
	commandTopic = "payment_commands"
	eventTopic   = "payment_events"
	poisonQueue  = "poison"
)

var (
	logger = watermill.NewStdLogger(false, false)
)

func main() {

	// configure router with middleware
	router, err := message.NewRouter(message.RouterConfig{}, logger)
	if err != nil {
		panic(err)
	}

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
	)

	// instantiate the handler
	processor := processor.NewStripeProcessor()
	takePaymentHandler := handler.NewClaimPayment(processor)

	// start publishing messages
	go publishMessages(publisher)

	// add a handler for taking payments to the router
	router.AddHandler(
		"process_payment_handler",
		commandTopic,
		subscriber,
		eventTopic,
		publisher,
		takePaymentHandler.Process,
	)

	// Run the router
	if err := router.Run(context.Background()); err != nil {
		panic(err)
	}
}

// publishMessages loops forever puttiing commands onto the command bus
func publishMessages(publisher message.Publisher) {

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

	for {
		time.Sleep(time.Duration(time.Second)) // throttle the messages for development

		msg := message.NewMessage(watermill.NewUUID(), buf)
		middleware.SetCorrelationID(watermill.NewUUID(), msg)

		log.Printf("sending message %s, correlation id: %s\n", msg.UUID, middleware.MessageCorrelationID(msg))

		if err := publisher.Publish(commandTopic, msg); err != nil {
			panic(err)
		}
	}
}

// [DEBUG] output information about a message
func printMessages(msg *message.Message) error {
	log.Printf("receive message %s payload: %s metadata: %v\n", msg.UUID, string(msg.Payload), msg.Metadata)
	return nil
}

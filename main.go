package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-amqp/pkg/amqp"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
)

const amqpURI = "amqp://guest:guest@localhost:5672/"
const topicName = "example.topic"
const takePaymentEndpoint = "http://localhost:8080/take-payment"

const goodCardNumber int = 13656978

var logger = watermill.NewStdLogger(false, false)

type takePaymentCommand struct {
	CardNumber int
}

func main() {
	amqpConfig := amqp.NewDurableQueueConfig(amqpURI)

	subscriber, err := amqp.NewSubscriber(amqpConfig, logger)
	if err != nil {
		panic(err)
	}
	defer subscriber.Close()

	messages, err := subscriber.Subscribe(context.Background(), topicName)
	if err != nil {
		panic(err)
	}

	go consume(messages)

	publisher, err := amqp.NewPublisher(amqpConfig, logger)
	if err != nil {
		panic(err)
	}
	defer publisher.Close()

	publishMessages(publisher)
}

// publishMessages loops forever puttiing commands onto the command bus
func publishMessages(publisher message.Publisher) {

	payload, err := json.Marshal(takePaymentCommand{CardNumber: goodCardNumber})
	if err != nil {
		panic(err)
	}

	for {
		msg := message.NewMessage(watermill.NewUUID(), payload)
		middleware.SetCorrelationID(watermill.NewUUID(), msg)

		if err := publisher.Publish(topicName, msg); err != nil {
			panic(err)
		}
	}
}

// consume loops forever pulling messages from the command bus and invoking takePayment each time
func consume(messages <-chan *message.Message) {

	for msg := range messages {
		if err := takePayment(); err != nil {
			log.Print(err)
		}

		log.Printf("consumed id: %s correllation id: %s payload: %s", msg.UUID, msg.Metadata.Get(middleware.CorrelationIDMetadataKey), string(msg.Payload))

		msg.Ack()
	}
}

// takePayment sends a POST request to the PSP
func takePayment() error {
	client := &http.Client{
		Timeout: time.Duration(time.Second * 1),
	}

	payload := bufio.NewReader(strings.NewReader(`{"card_number": ` + strconv.Itoa(goodCardNumber) + `}`))

	req, err := http.NewRequest(http.MethodPost, takePaymentEndpoint, payload)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to take the payment, got response code %d", resp.StatusCode)
	}

	if _, err = client.Do(req); err != nil {
		return err
	}

	return nil
}

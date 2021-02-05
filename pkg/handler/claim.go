package handler

import (
	"fmt"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/golang/protobuf/proto"
	"github.com/mannion007/payments-prototype/pkg/payment"
)

// ClaimPayment is a message handler which takes payments
type ClaimPayment struct {
	Processor payment.Processor
}

//Process handles messages using a Processor, returning a resulting message and an error, if any
func (tph ClaimPayment) Process(msg *message.Message) ([]*message.Message, error) {

	var claim payment.Claim

	err := proto.Unmarshal(msg.Payload, &claim)
	if err != nil {
		panic(err)
	}

	outcome, err := tph.Processor.Process(&claim)
	if err != nil {
		return nil, fmt.Errorf("error when processing command [%s]", err)
	}

	payload, err := proto.Marshal(outcome)
	if err != nil {
		return nil, fmt.Errorf("failed to encode outcome %s", err.Error())
	}

	event := message.NewMessage(watermill.NewUUID(), payload)

	return message.Messages{event}, nil
}

// NewClaimPayment is a fatory for the handler: TakePayment
func NewClaimPayment(processor payment.Processor) *ClaimPayment {

	handler := ClaimPayment{
		Processor: processor,
	}

	return &handler
}

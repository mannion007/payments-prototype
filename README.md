# Payment Prototype

This is a prototye project to explore the potential of using golang and [watermill](https://watermill.io/) to build a message driven microservice.


# How to run

Configure the application by adding the stripe key in stripe.go


Bring up the dependencies (wiremock and rabbit) with 

```
docker-compose up -d
```

Start the application
```
go run main.go
```

An example request
```
curl --location -vvv 'localhost:8888/pay' --data-raw '{
    "idempotency_token": "ed665eb7-4ced-446e-a77f-88487f42ec1f",
    "payee_id": "fbc8fa45-9041-42ea-abe0-2dc9c7581123",
    "amount": {
        "currency": "GBP",
        "value": 9999
    },
    "card": {
        "number": "4242424242424242",
        "expiry": {
            "year": "2023",
            "month": "10"
        }
    }
}'
```
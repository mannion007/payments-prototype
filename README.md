# Payment Prototype

This is a prototye project to explore the potential of using golang to build a message driven microservice.

## improvements / todo

Everything is roughed out in the main file, as this is a prototype, naturally everything needs splitting out, interfaces introducing, tests etc
All message processing is done in a single go routine, introducing the worker pattern could improve throughput significantly
[watermill](https://watermill.io/) has many more features than this prototype uses, a real application would use a router with a combonation of the useful middleware

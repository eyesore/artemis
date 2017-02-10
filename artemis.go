// Package artemis manages Socket connections and provides an API for handling incoming messages,
// sending messages back to the client, and subscribing to and firing server-side events.
//
// artemis is a wordification of rtmes, which stands for Real Time Message & Event Server
package artemis

import (
	"encoding/json"
	"errors"
	"log"
	"time"
)

// TODO support multiple socket protocols

var (
	// DefaultTextParser can be overridden to implement text parsing for Client without
	// providing a custom parser
	DefaultTextParser = ParseJSONMessage

	// Timeout is the time allowed to write messages
	Timeout     = 10 * time.Second
	pongTimeout = Timeout * 6
	pingPeriod  = (pongTimeout * 9) / 10

	// Default WS configs - can be set at package level
	// TODO, update for multiple protocols
	ReadLimit                       int64 = 4096
	HandshakeTimeout                      = 10 * time.Second
	ReadBufferSize, WriteBufferSize int

	// Errors sends errors encountered during send and receive and is meant to be consumed by a logger
	// TODO provide default logger to stdout
	Errors   = make(chan error, 256)
	Warnings = make(chan error, 256)

	// ErrHubMismatch occurs when trying to add a client to family with a different hub.
	ErrHubMismatch = errors.New("Unable to add a client to a family in a different hub.")

	// ErrDuplicateClient occurs when a client ID matches an existing client ID in the family on join.
	ErrDuplicateClient = errors.New("Tried to add a duplicate client to family.")

	// ErrAlreadySubscribed occurs when trying to add an event handler to a Responder that already has one.
	ErrAlreadySubscribed = errors.New("Trying to add duplicate event to responder.")

	// ErrUnparseableMessage indicates that a message does not contain some expected data.
	ErrUnparseableMessage = errors.New("The message parser does not recognize the message format.")

	// ErrDuplicateAction means that a MessageResponder is already listening to perform the same action
	// in response to the same message
	ErrDuplicateAction = errors.New("An action already exists for that event or message name.")

	ErrIllegalPingTimeout = errors.New("pingPeriod must be shorter than pongTimeout")

	ErrEventChannelHasClosed = errors.New("This client is no longer receiving events.")

	ErrNoSubscribers = errors.New("Hub fired event but no one is listening.")

	errNotYetImplemented = errors.New("You are trying to use a feature that has not been implemented yet.")
)

// initialize logging to STDOUT
// TODO, setuplogger and allow override
func init() {
	go func() {
		// log errors and warnings
		for {
			select {
			case w := <-Warnings:
				log.Println(w)
			case e := <-Errors:
				log.Println(e)
			}
		}
	}()
}

func warn(e error) {
	go sendWarning(e)
}

func throw(e error) {
	go sendError(e)
}

func sendWarning(e error) {
	// TODO write artemis prefix to all outgoing messages
	Warnings <- e
}

func sendError(e error) {
	// TODO write artemis prefix to all outgoing messages
	Errors <- e
}

// SetPingPeriod allows the application to specify the period between sending ping messages to clients
func SetPingPeriod(n time.Duration) error {
	if n >= pongTimeout {
		return ErrIllegalPingTimeout
	}
	pingPeriod = n
	return nil
}

// SetPongTimeout allows the application to specify the period allowed to receive a pong message from clients
func SetPongTimeout(n time.Duration) error {
	if n <= pingPeriod {
		return ErrIllegalPingTimeout
	}
	pongTimeout = n
	return nil
}

// ParseJSONMessage parses a Message from a byte stream if possible.
func ParseJSONMessage(m []byte) (*Message, error) {
	var messageName interface{}
	var ok bool
	pm := make(map[string]interface{})
	err := json.Unmarshal(m, &pm)
	if err != nil {
		return nil, err
	}
	if messageName, ok = pm["name"]; !ok {
		return nil, ErrUnparseableMessage
	}
	mdg := &Message{messageName.(string), pm, m}

	return mdg, err
}

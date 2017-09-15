// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import "github.com/tbogdala/filefreezer/cmd/freezer/models"

// commandState tracks the state of the freezer commands during execution.
type commandState struct {
	// the host URI used for calls
	hostURI string

	// the authentication token returned after logging in
	authToken string

	// the stored crypto hash for the client that is used
	// to verify the client-entered password.
	cryptoHash []byte

	// the key used for cryptography
	cryptoKey []byte

	// the capabilities returned by the authenticated server
	serverCapabilities models.ServerCapabilities
}

// newCommandState creates a new CommandState object.
func newCommandState() *commandState {
	s := new(commandState)
	return s
}

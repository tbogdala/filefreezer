// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package command

import (
	"log"

	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// State tracks the state of the freezer commands during execution.
type State struct {
	// the host URI used for calls
	HostURI string

	// the authentication token returned after logging in
	AuthToken string

	// the stored crypto hash for the client that is used
	// to verify the client-entered plaintext password.
	CryptoHash []byte

	// the key used to encrypt/decrypt file chunks and names
	// and is derived from a plaintext password.
	CryptoKey []byte

	// the capabilities returned by the authenticated server
	ServerCapabilities models.ServerCapabilities

	// an overridable Println implementation that defaults to using
	// the log package version from the stdlib.
	Println func(v ...interface{})

	// an overridable Printf implementation that defaults to using
	// the log package version from the stdlib.
	Printf func(format string, v ...interface{})

	// the HTTPS TLS public crt file
	TLSCrt string

	// the HTTPS TLS private key file
	TLSKey string

	// extra strict file checking during sync operations
	ExtraStrict bool
}

// NewState creates a new State object.
func NewState() *State {
	s := new(State)
	s.SetQuiet(false)
	return s
}

func defaultPrintln(v ...interface{}) {
	log.Println(v...)
}

func defaultPrintf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// SetQuiet will alter the Printf and Println functions to either
// write or suppress output depending on the quiet flag.
func (s *State) SetQuiet(quiet bool) {
	if quiet {
		s.Printf = func(format string, v ...interface{}) {}
		s.Println = func(v ...interface{}) {}
	} else {
		s.Println = defaultPrintln
		s.Printf = defaultPrintf
	}
}

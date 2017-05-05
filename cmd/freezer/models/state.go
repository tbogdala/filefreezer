// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package models

import (
	"github.com/tbogdala/filefreezer"
)

// State represents the server state and includes configuration flags.
type State struct {
	// DatabasePath is the file path to the database used for storage
	DatabasePath string

	// DefaultQuota is the default quota size for a user
	DefaultQuota int

	// Port is the port to listen to
	Port int

	// PublicKeyPath is the file path to the public crypto key
	PublicKeyPath string

	// PrivateKeyPath is the file path to the private crypto key
	PrivateKeyPath string

	// SignKey is the loaded crypto key for signing security tokens
	SignKey []byte

	// VerifyKey is the loaded crypto key for verifying security tokens
	VerifyKey []byte

	// Storage is the filefreezer storage object used to keep data
	Storage *filefreezer.Storage

	// Authorizor is the interface able to verify username and passwords
	// as well as sign username and ids into a authentication token.
	Authorizor
}

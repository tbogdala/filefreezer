// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package models

import (
	"net/http"

	"github.com/dgrijalva/jwt-go"
	"github.com/tbogdala/filefreezer"
)

// Authorizor is an interface specifying functions needed to create
// authentication tokens for a given username and userID as well
// as performing password verification
type Authorizor interface {
	GenerateToken(username string, userID int) (string, error)
	GetUserFromToken(token *jwt.Token) (string, int)
	VerifyPassword(username, password string) (*filefreezer.User, error)
	VerifyToken(r *http.Request) (*jwt.Token, error)
}

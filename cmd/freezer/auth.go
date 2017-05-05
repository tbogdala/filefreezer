// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/tbogdala/filefreezer"
)

// JWTAuthenticator implements models.TokenGenerator for JWT tokens
type JWTAuthenticator struct {
	Storage      *filefreezer.Storage
	TokenTimeout time.Duration
	privKey      *rsa.PrivateKey
}

// NewJWTAuthenticator creates a new JWTAuthenticator object to sign tokens.
func NewJWTAuthenticator(storage *filefreezer.Storage, signKey []byte) (*JWTAuthenticator, error) {
	var err error
	ta := new(JWTAuthenticator)
	ta.Storage = storage
	ta.TokenTimeout = time.Minute * 20
	ta.privKey, err = jwt.ParseRSAPrivateKeyFromPEM(signKey)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate the authentication private rsa key: %v", err)
	}

	return ta, nil
}

// VerifyPassword pulls the user information from Storage for the username
// provided and then attempts to verify the password string against the stored
// salted password hash. A non-nil User value is returned if the password is correct.
func (auth *JWTAuthenticator) VerifyPassword(username, password string) (*filefreezer.User, error) {
	user, err := auth.Storage.GetUser(username)
	if err != nil {
		return nil, fmt.Errorf("Could not find user in the database")
	}

	verified := filefreezer.VerifyPassword(password, user.Salt, user.SaltedHash)
	if !verified {
		return nil, fmt.Errorf("could not verify the user against the stored salted hash")
	}

	return user, nil
}

// GenerateToken makes a JWT token for the username specified
func (auth *JWTAuthenticator) GenerateToken(username string, userID int) (string, error) {
	// create a signer for RSA256
	gen := jwt.New(jwt.GetSigningMethod("RS256"))
	claims := gen.Claims.(jwt.MapClaims)

	// set the claims
	claims["iss"] = "filefreezer"
	claims["filefreezer.username"] = username
	claims["filefreezer.userid"] = userID
	claims["exp"] = time.Now().Add(auth.TokenTimeout).Unix()

	// make the token string to return
	token, err := gen.SignedString(auth.privKey)
	if err != nil {
		return "", fmt.Errorf("Failed to generate the authentication token. %v", err)
	}

	return token, nil
}

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"time"

	"github.com/dgrijalva/jwt-go"
	jwtrequest "github.com/dgrijalva/jwt-go/request"
	"github.com/tbogdala/filefreezer"
)

// JWTAuthenticator implements models.TokenGenerator for JWT tokens
type JWTAuthenticator struct {
	Storage      *filefreezer.Storage
	TokenTimeout time.Duration
	privKey      *rsa.PrivateKey
	pubKey       *rsa.PublicKey
}

// NewJWTAuthenticator creates a new JWTAuthenticator object to sign tokens.
func NewJWTAuthenticator(storage *filefreezer.Storage, signKey []byte, verifyKey []byte) (*JWTAuthenticator, error) {
	var err error
	ta := new(JWTAuthenticator)
	ta.Storage = storage
	ta.TokenTimeout = time.Minute * 20
	ta.privKey, err = jwt.ParseRSAPrivateKeyFromPEM(signKey)
	ta.pubKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyKey)
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

// UserClaims is used to authenticate API traffic with JWT tokens
type UserClaims struct {
	*jwt.StandardClaims
	UserID   int
	Username string
}

// GenerateToken makes a JWT token for the username specified
func (auth *JWTAuthenticator) GenerateToken(username string, userID int) (string, error) {
	// create a signer for RSA256
	gen := jwt.New(jwt.GetSigningMethod("RS256"))
	gen.Claims = &UserClaims{
		&jwt.StandardClaims{
			ExpiresAt: time.Now().Add(auth.TokenTimeout).Unix(),
		},
		userID,
		username,
	}

	// make the token string to return
	token, err := gen.SignedString(auth.privKey)
	if err != nil {
		return "", fmt.Errorf("Failed to generate the authentication token. %v", err)
	}

	return token, nil
}

// VerifyToken returns a JWT token object and a nil error return value if the
// authentication token was verified successfully; otherwise an error object
// is returned.
func (auth *JWTAuthenticator) VerifyToken(r *http.Request) (*jwt.Token, error) {
	// validate the token
	token, err := jwtrequest.ParseFromRequestWithClaims(r, jwtrequest.OAuth2Extractor, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		// since we only use the one private key to sign the tokens,
		// we also only use its public counter part to verify
		return auth.pubKey, nil
	})

	// If the token is missing or invalid, return error
	if err != nil {
		return nil, err
	}

	return verifyCheck(token, err)
}

// GetUserFromToken returns the username from the token claim.
// NOTE: assumes that the token has been authenticated already and if the
// token is not valid then an empty string is returned.
func (auth *JWTAuthenticator) GetUserFromToken(token *jwt.Token) (string, int) {
	if token == nil || token.Valid == false || token.Claims == nil {
		return "", 0
	}

	claims, okay := token.Claims.(*UserClaims)
	if !okay {
		return "", 0
	}
	return claims.Username, claims.UserID
}

func verifyCheck(token *jwt.Token, err error) (*jwt.Token, error) {
	if err != nil {
		switch err.(type) {
		case *jwt.ValidationError:
			vErr := err.(*jwt.ValidationError)
			switch vErr.Errors {
			case jwt.ValidationErrorExpired:
				return nil, fmt.Errorf("authentication token hjas expired; log in again")
			default:
				return nil, fmt.Errorf("error while parsing the authentication token (validation error)")
			}
		default:
			return nil, fmt.Errorf("error while parsing the authentication token")
		}
	}

	if token == nil || token.Valid == false {
		return nil, fmt.Errorf("invalid authentication token")
	}

	// TODO: should check to see if the token is expired.

	return token, nil
}

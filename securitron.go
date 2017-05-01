// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package filefreezer

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// GenSaltedHash takes the user password, generates a new random salt,
// then generates a hash from the salted password combination.
func GenSaltedHash(unsaltedPassword string) (salt string, saltedhash []byte, err error) {
	// generate a 32 byte salt
	salt, err = getSalt(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate salt for salted password: %v", err)
	}

	saltedhash, err = bcrypt.GenerateFromPassword([]byte(unsaltedPassword+salt), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate hash for salted password: %v", err)
	}

	return
}

// VerifyPassword takes the user-supplied unsalted password and the stored salt and hash
// and verifies that the supplied unsalted password is the correct match. Returns true on
// match and false on fail.
func VerifyPassword(unsaltedPassowrd string, salt string, saltedHash []byte) bool {
	err := bcrypt.CompareHashAndPassword(saltedHash, []byte(unsaltedPassowrd+salt))
	if err == nil {
		return true
	}

	return false
}

func getSalt(n int) (string, error) {
	// generate n-number of crypto random bytes
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to get random salt bytes: %v", err)
	}

	return base64.URLEncoding.EncodeToString(b), nil
}

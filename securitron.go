// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package filefreezer

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"

	"golang.org/x/crypto/bcrypt"
)

// CalcFileHashInfo takes the file name and calculates the number of chunks, last modified time
// and hash string for the file. An error is returned on failure.
func CalcFileHashInfo(maxChunkSize int64, filename string) (chunkCount int, lastMod int64, permissions uint32, hashString string, e error) {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		e = fmt.Errorf("failed to stat the local file (%s) for the test", filename)
		return
	}

	lastMod = fileInfo.ModTime().UTC().Unix()
	permissions = uint32(fileInfo.Mode())

	// calculate the chunk count required for the file size
	fileSize := fileInfo.Size()
	chunkCount = int(fileSize / maxChunkSize)
	if fileSize%maxChunkSize != 0 {
		chunkCount++
	}

	// generate a hash for the test file
	hasher := sha1.New()
	fileBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		e = fmt.Errorf("failed to create a file byte array for the hashing operation")
		return
	}
	hasher.Write(fileBytes)
	hash := hasher.Sum(nil)
	hashString = base64.URLEncoding.EncodeToString(hash)

	return
}

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

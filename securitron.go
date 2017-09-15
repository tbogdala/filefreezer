// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package filefreezer

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"strconv"
	"strings"

	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/scrypt"
)

const (
	defaultPasswordCost = 10 // analogus to bcrypt's DefaultCost
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

// GenLoginPasswordHash takes the user password, generates a new random salt,
// then generates a hash from the salted password combination.
func GenLoginPasswordHash(unsaltedPassword string) (salt string, saltedhash []byte, err error) {
	// generate a 32 byte salt
	salt, err = getSalt(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate salt for salted password: %v", err)
	}

	// technically, bcrypt does its own salting. This is just double salt or maybe some pepper.
	saltedhash, err = bcrypt.GenerateFromPassword([]byte(unsaltedPassword+salt), defaultPasswordCost)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate hash for salted password: %v", err)
	}

	return
}

// VerifyLoginPassword takes the user-supplied unsalted password and the stored salt and hash
// and verifies that the supplied unsalted password is the correct match. Returns true on
// match and false on fail.
func VerifyLoginPassword(unsaltedPassowrd string, salt string, saltedHash []byte) bool {
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

// GenCryptoPasswordHash takes the user password then generates a crytpo hash. If makeKeyHash
// is false, only the key parameter is generated. If keyHashOpts is not an empty string,
// it attempts to split the string by '$' dividers for scrypt parameters. NOTE: it's
// intended that keyHashOpts will be the keyHashCombo return value of a previous call.
func GenCryptoPasswordHash(password string, makeKeyHash bool, keyHashOpts string) (key []byte, keyHash []byte, keyHashCombo string, err error) {
	// scrypt parameters
	n := 16384 * 2 * 2 * 2 // CPU/memory cost parameter (logN)
	r := 8                 // block size parameter (octets)
	p := 1                 // parallelisation parameter (positive int)
	salt := make([]byte, 16)

	// override these if keyHashOpts is supplied so that the same salt and settings
	// are used to generate keys.
	if keyHashOpts != "" {
		vals := strings.Split(keyHashOpts, "$")
		n, err = strconv.Atoi(vals[0])
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to parse the crypto password hashing 'n' option: %v", err)
		}

		r, err = strconv.Atoi(vals[1])
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to parse the crypto password hashing 'r' option: %v", err)
		}

		p, err = strconv.Atoi(vals[2])
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to parse the crypto password hashing 'p' option: %v", err)
		}

		salt, err = hex.DecodeString(vals[3])
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to parse the crypto password hashing salt: %v", err)
		}
	} else {
		// no salt provided so we must farm our own from the random fields
		_, err = rand.Read(salt)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to get random salt bytes: %v", err)
		}
	}

	key, err = scrypt.Key([]byte(password), salt, n, r, p, 32)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to generate the key for crypto password: %v", err)
	}

	if makeKeyHash {
		keyHash, err = scrypt.Key(key, salt, n, r, p, 32)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to generate the key hash for crypto password: %v", err)
		}

		keyHashCombo = fmt.Sprintf("%d$%d$%d$%x$%x", n, r, p, salt, keyHash)
	}

	return
}

// VerifyCryptoPassword takes a plain text password and compares it against a hash
// of the crypto key to verify that the password is correct and the crypto key is
// the correct one. On success and successful match a non-nil []byte slice is returned.
// If the keys do not match nil is returned. Otherwise an non-nil error is returned.
func VerifyCryptoPassword(password string, keyHashCombo string) ([]byte, error) {
	key, keyHash, _, err := GenCryptoPasswordHash(password, true, keyHashCombo)
	if err != nil {
		return nil, fmt.Errorf("failed to generate the crypto key to check against the stored hash: %v", err)
	}

	vals := strings.Split(keyHashCombo, "$")
	storedKeyHash, err := hex.DecodeString(vals[4])
	if err != nil {
		return nil, fmt.Errorf("failed to parse the stored crypto key hash: %v", err)
	}

	if subtle.ConstantTimeCompare(storedKeyHash, keyHash) != 1 {
		return nil, nil
	}

	return key, nil
}

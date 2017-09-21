// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package command

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

const (
	cryptoNonceSize = 12
)

// encryptString will encrypt the source string bytes and then return
// a base64 encoded string version of the crypto bytes
func (s *State) EncryptString(source string) (string, error) {
	cryptoBytes, err := s.encryptBytes([]byte(source))
	if err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(cryptoBytes)
	return encoded, nil
}

// decryptString will decrypt the source base64 encoded string into
// crypto bytes and then return the result as a string.
func (s *State) DecryptString(encoded string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	decrypted, err := s.decryptBytes(decoded)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

func (s *State) encryptBytes(b []byte) ([]byte, error) {
	// encrypt the original bytes
	aesCipher, err := aes.NewCipher(s.CryptoKey)
	if err != nil {
		return nil, fmt.Errorf("Couldn't initialize the AES cipher. " + err.Error())
	}

	gcm, err := cipher.NewGCM(aesCipher)
	if err != nil {
		return nil, fmt.Errorf("Couldn't initialize the AES-GCM cipher. " + err.Error())
	}

	nonce := make([]byte, cryptoNonceSize)
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, fmt.Errorf("Failed to initialize random data for AES-GCM. " + err.Error())
	}

	cipherBytes := gcm.Seal(nil, nonce, b, nil)
	cipherBytes = append(nonce, cipherBytes...)
	return cipherBytes, nil
}

func (s *State) decryptBytes(b []byte) ([]byte, error) {
	// encrypt the original bytes
	aesCipher, err := aes.NewCipher(s.CryptoKey)
	if err != nil {
		return nil, fmt.Errorf("Couldn't initialize the AES cipher. " + err.Error())
	}

	gcm, err := cipher.NewGCM(aesCipher)
	if err != nil {
		return nil, fmt.Errorf("Couldn't initialize the AES-GCM cipher. " + err.Error())
	}

	nonce := make([]byte, cryptoNonceSize)
	copy(nonce, b[:cryptoNonceSize])
	clearBytes, err := gcm.Open(nil, nonce, b[cryptoNonceSize:], nil)
	return clearBytes, err
}

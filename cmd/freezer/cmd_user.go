// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// addUser adds a user to the database using the username, password and quota provided.
// The store object will take care of generating the salt and salted password.
func (s *commandState) addUser(store *filefreezer.Storage, username string, password string, quota int) *filefreezer.User {
	// generate the salt and salted login password hash
	salt, saltedPass, err := filefreezer.GenLoginPasswordHash(password)
	if err != nil {
		log.Fatalf("Failed to generate a password hash %v", err)
	}

	// add the user to the database with CryptoHash empty as that will be
	// set by the client.
	user, err := store.AddUser(username, salt, saltedPass, quota)
	if err != nil {
		log.Fatalf("Failed to create the user %s: %v", username, err)
	}

	log.Println("User created successfully")
	return user
}

// rmUser removes a user from the database using the username as akey.
func (s *commandState) rmUser(store *filefreezer.Storage, username string) error {
	// add the user to the database
	err := store.RemoveUser(username)
	if err != nil {
		log.Fatalf("Failed to remove the user %s: %v", username, err)
	}

	log.Println("User removed successfully")
	return nil
}

// modUser modifies a user in the database. if the newQuota, newUsername or newPassword
// fields are non-nil then their values are updated in the database.
func (s *commandState) modUser(store *filefreezer.Storage, username string, newQuota int, newUsername string, newPassword string) {
	// get existing user
	user, err := store.GetUser(username)
	if err != nil {
		log.Fatalf("Failed to get an existing user with the name %s: %v", username, err)
	}
	stats, err := store.GetUserStats(user.ID)
	if err != nil {
		log.Fatalf("Failed to get an existing user stats with the name %s: %v", username, err)
	}

	updatedName := user.Name
	if newUsername != "" {
		updatedName = newUsername
	}

	updatedSalt := user.Salt
	updatedSaltedHash := user.SaltedHash
	if newPassword != "" {
		updatedSalt, updatedSaltedHash, err = filefreezer.GenLoginPasswordHash(newPassword)
		if err != nil {
			log.Fatalf("Failed to generate a password hash %v", err)
		}
	}

	updatedQuota := stats.Quota
	if newQuota > 0 {
		updatedQuota = newQuota
	}

	// update the user in the database ... but don't update CryptoHash. that's only done client side
	// and through the web API.
	err = store.UpdateUser(user.ID, updatedName, updatedSalt, updatedSaltedHash, user.CryptoHash, updatedQuota)
	if err != nil {
		log.Fatalf("Failed to modify the user %s: %v", username, err)
	}

	log.Println("User modified successfully")
}

func (s *commandState) getUserStats() (stats filefreezer.UserStats, e error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/user/stats", s.hostURI)
	body, err := runAuthRequest(target, "GET", s.authToken, nil)
	var r models.UserStatsGetResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		e = fmt.Errorf("Failed to get the user stats: %v", err)
		return
	}

	log.Printf("Quota:     %v\n", r.Stats.Quota)
	log.Printf("Allocated: %v\n", r.Stats.Allocated)
	log.Printf("Revision:  %v\n", r.Stats.Revision)

	stats = r.Stats
	return
}

func (s *commandState) getAllFileHashes() ([]filefreezer.FileInfo, error) {
	target := fmt.Sprintf("%s/api/files", s.hostURI)
	body, err := runAuthRequest(target, "GET", s.authToken, nil)
	if err != nil {
		return nil, err
	}

	var allFiles models.AllFilesGetResponse
	err = json.Unmarshal(body, &allFiles)
	if err != nil {
		return nil, fmt.Errorf("Poorly formatted response to %s: %v", target, err)
	}

	return allFiles.Files, nil
}

func (s *commandState) setCryptoHashForPassword(cryptoPassword string) error {
	// first we derive the crypto password bytes that are derived from the password text
	_, _, combinedHashString, err := filefreezer.GenCryptoPasswordHash(cryptoPassword, true, "")
	if err != nil {
		return fmt.Errorf("Failed to generate the cryptography key from the password: %v", err)
	}

	var putReq models.UserCryptoHashUpdateRequest
	putReq.CryptoHash = []byte(combinedHashString)

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/user/cryptohash", s.hostURI)
	body, err := runAuthRequest(target, "PUT", s.authToken, putReq)
	var r models.UserCryptoHashUpdateResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return fmt.Errorf("Failed to set the user's cryptography password hash: %v", err)
	}

	if r.Status != true {
		return fmt.Errorf("an unknown error occurred while updating the cryptography password")
	}

	s.cryptoHash = putReq.CryptoHash
	log.Printf("Hash of cryptography password updated successfully.")
	return nil
}

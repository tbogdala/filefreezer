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

// runAddUser adds a user to the database using the username, password and quota provided.
// The store object will take care of generating the salt and salted password.
func runAddUser(store *filefreezer.Storage, username string, password string, quota int) *filefreezer.User {
	// generate the salt and salted password hash
	salt, saltedPass, err := filefreezer.GenSaltedHash(password)
	if err != nil {
		log.Fatalf("Failed to generate a password hash %v", err)
	}

	// add the user to the database
	user, err := store.AddUser(username, salt, saltedPass, quota)
	if err != nil {
		log.Fatalf("Failed to create the user %s: %v", username, err)
	}

	log.Println("User created successfully")
	return user
}

// runModUser modifies a user in the database. if the newQuota, newUsername or newPassword
// fields are non-nil then their values are updated in the database.
func runModUser(store *filefreezer.Storage, username string, newQuota int, newUsername string, newPassword string) {
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
		updatedSalt, updatedSaltedHash, err = filefreezer.GenSaltedHash(newPassword)
		if err != nil {
			log.Fatalf("Failed to generate a password hash %v", err)
		}
	}

	updatedQuota := stats.Quota
	if newQuota > 0 {
		updatedQuota = newQuota
	}

	// update the user in the database
	err = store.UpdateUser(user.ID, updatedName, updatedSalt, updatedSaltedHash, updatedQuota)
	if err != nil {
		log.Fatalf("Failed to modify the user %s: %v", username, err)
	}

	log.Println("User modified successfully")
}

func runUserStats(hostURI string, token string) (stats filefreezer.UserStats, e error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/user/stats", hostURI)
	body, err := runAuthRequest(target, "GET", token, nil)
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

func runGetAllFileHashes(hostURI, token string) ([]filefreezer.FileInfo, error) {
	target := fmt.Sprintf("%s/api/files", hostURI)
	body, err := runAuthRequest(target, "GET", token, nil)
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

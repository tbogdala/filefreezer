// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package command

import (
	"encoding/json"
	"fmt"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// AddUser adds a user to the database using the username, password and quota provided.
// The store object will take care of generating the salt and salted password.
func (s *State) AddUser(store *filefreezer.Storage, username string, password string, quota int) (*filefreezer.User, error) {
	// generate the salt and salted login password hash
	salt, saltedPass, err := filefreezer.GenLoginPasswordHash(password)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate a password hash %v", err)
	}

	// add the user to the database with CryptoHash empty as that will be
	// set by the client.
	user, err := store.AddUser(username, salt, saltedPass, quota)
	if err != nil {
		return nil, fmt.Errorf("Failed to create the user %s: %v", username, err)
	}

	s.Println("User created successfully")
	return user, nil
}

// RmUser removes a user from the database using the username as akey.
func (s *State) RmUser(store *filefreezer.Storage, username string) error {
	// add the user to the database
	err := store.RemoveUser(username)
	if err != nil {
		return fmt.Errorf("Failed to remove the user %s: %v", username, err)
	}

	s.Println("User removed successfully")
	return nil
}

// ModUser modifies a user in the database. if the newQuota, newUsername or newPassword
// fields are non-nil then their values are updated in the database.
func (s *State) ModUser(store *filefreezer.Storage, username string, newQuota int, newUsername string, newPassword string) error {
	// get existing user
	user, err := store.GetUser(username)
	if err != nil {
		return fmt.Errorf("Failed to get an existing user with the name %s: %v", username, err)
	}
	stats, err := store.GetUserStats(user.ID)
	if err != nil {
		return fmt.Errorf("Failed to get an existing user stats with the name %s: %v", username, err)
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
			return fmt.Errorf("Failed to generate a password hash %v", err)
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
		return fmt.Errorf("Failed to modify the user %s: %v", username, err)
	}

	s.Println("User modified successfully")
	return nil
}

// GetUserStats returns a UserStats object for the authenticated user
// in the command State. A non-nil error value is returned on failure.
func (s *State) GetUserStats() (stats filefreezer.UserStats, e error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/user/stats", s.HostURI)
	body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
	var r models.UserStatsGetResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		e = fmt.Errorf("Failed to get the user stats: %v", err)
		return
	}

	s.Printf("Quota:     %v\n", r.Stats.Quota)
	s.Printf("Allocated: %v\n", r.Stats.Allocated)
	s.Printf("Revision:  %v\n", r.Stats.Revision)

	stats = r.Stats
	return
}

// GetAllFileHashes returns a slice of FileInfo objects for all files registered
// to the authenticated user in the command State. A non-nil error value is
// returned on failure.
func (s *State) GetAllFileHashes() ([]filefreezer.FileInfo, error) {
	target := fmt.Sprintf("%s/api/files", s.HostURI)
	body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
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

// SetCryptoHashForPassword sets the hash of the hash of the plaintext password on
// the server for the authenticated user in the command State. This can then
// be used to ensure the plaintext password entered by a user is the correct one
// to decrypt the files without actually storing the crypto key (the hashed plaintext
// password) on the server. A non-nil error value is returned on failure.
func (s *State) SetCryptoHashForPassword(cryptoPassword string) error {
	// first we derive the crypto password bytes that are derived from the password text
	_, _, combinedHashString, err := filefreezer.GenCryptoPasswordHash(cryptoPassword, true, "")
	if err != nil {
		return fmt.Errorf("Failed to generate the cryptography key from the password: %v", err)
	}

	var putReq models.UserCryptoHashUpdateRequest
	putReq.CryptoHash = []byte(combinedHashString)

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/user/cryptohash", s.HostURI)
	body, err := s.RunAuthRequest(target, "PUT", s.AuthToken, putReq)
	if err != nil {
		return fmt.Errorf("http request to set the user's cryptohash failed: %v", err)
	}

	var r models.UserCryptoHashUpdateResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return fmt.Errorf("Failed to set the user's cryptography password hash: %v", err)
	}

	if r.Status != true {
		return fmt.Errorf("an unknown error occurred while updating the cryptography password")
	}

	s.CryptoHash = putReq.CryptoHash
	s.Println("Hash of cryptography password updated successfully.")
	return nil
}

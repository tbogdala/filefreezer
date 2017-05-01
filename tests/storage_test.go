// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package tests

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"testing"

	"github.com/tbogdala/filefreezer"
)

func TestBasicDBCreation(t *testing.T) {
	// create an in memory storage
	store, err := filefreezer.NewStorage("file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("Failed to create the in-memory storage for testing. %v", err)
	}
	defer store.Close()

	// setup the tables in test database
	err = store.CreateTables()
	if err != nil {
		t.Fatalf("Failed to create tables for testing. %v", err)
	}

	// test to make sure calling this again fails
	err = store.CreateTables()
	if err == nil {
		t.Fatal("Create duplicate tables worked when it should return an error")
	}

	///////////////////////////////////////////////////////////////////////////
	// User registration
	setupTestUser(store, "admin", "hamster", t)

	// make sure a duplicate user fails
	success, err := store.AddUser("admin", "99999", []byte{1, 2, 3, 4, 5})
	if err == nil || success {
		t.Fatal("Should have failed to add a duplicate user but did not.")
	}

	// a second, legit user should be okay
	setupTestUser(store, "admin2", "hamster2", t)

	// these calls for made up users should fail
	_, _, _, err = store.GetUser("ghost")
	if err == nil {
		t.Fatal("GetUser succeeded with a user that shouldn't exist in the database.")
	}
	_, err = store.GetUserQuota(777)
	if err == nil {
		t.Fatal("GetUserQuota succeeded with a user that shouldn't exist in the database.")
	}
	_, _, err = store.GetUserInfo(777)
	if err == nil {
		t.Fatal("GetUserInfo succeeded with a user that shouldn't exist in the database.")
	}
	err = store.UpdateUserInfo(777, 512)
	if err == nil {
		t.Fatal("UpdateUserInfo succeeded with a user that shouldn't exist in the database.")
	}

	///////////////////////////////////////////////////////////////////////////
	// File manipulation

	// get user credentials
	user := "admin"
	userID, _, _, err := store.GetUser(user)
	if err != nil {
		t.Fatal("GetUser failed to get the admin test user.")
	}

	// pull up the local file information
	filename := "../README.md"
	chunkCount, lastMod, hashString := calcFileHashInfo(t, store.ChunkSize, filename)

	// add the file information to the storage server
	err = store.AddFileInfo(userID, filename, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", filename, err)
	}

	// get all the file info objects
	fileInfos, err := store.GetAllUserFileInfos(userID)
	if err != nil {
		t.Fatalf("Failed to get all of the user (id:%d) file infos in storage: %v", userID, err)
	}
	if len(fileInfos) != 1 {
		t.Fatalf("Returned the wrong number of file infos (%d) for a user (id:%d).", len(fileInfos), userID)
	}
	first := fileInfos[0]
	if first.UserID != userID || first.FileID != 1 || first.FileName != filename ||
		first.ChunkCount != chunkCount || first.LastMod != lastMod || first.FileHash != hashString {
		t.Fatalf("The file information returned %s was incorrect: %v", filename, first)
	}

	// try to get the file again, but by ID
	fileByID, err := store.GetFileInfo(first.FileID)
	if err != nil || first.UserID != fileByID.UserID || first.FileID != fileByID.FileID ||
		first.ChunkCount != fileByID.ChunkCount || first.FileName != fileByID.FileName ||
		first.LastMod != fileByID.LastMod || first.FileHash != fileByID.FileHash {
		t.Fatalf("Failed to get the added file using the fileID (%d) returned by GetAllUserFileInfos(): %v", first.FileID, err)
	}

	// try getting a bad file id
	_, err = store.GetFileInfo(777)
	if err == nil {
		t.Fatal("Getting a user file info object for a non-existant fileID succeeded.")
	}

	// add a second file
	filename = "../storage.go"
	chunkCount, lastMod, hashString = calcFileHashInfo(t, store.ChunkSize, filename)

	// add the file information to the storage server
	err = store.AddFileInfo(userID, filename, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", filename, err)
	}

	// get all the file info objects again
	fileInfos, err = store.GetAllUserFileInfos(userID)
	if err != nil {
		t.Fatalf("Failed to get all of the user (id:%d) file infos in storage: %v", userID, err)
	}
	if len(fileInfos) != 2 {
		t.Fatalf("Returned the wrong number of file infos (%d) for a user (id:%d).", len(fileInfos), userID)
	}
	first = fileInfos[0]
	second := fileInfos[1]
	if second.FileName != filename {
		first = fileInfos[1]
		second = fileInfos[0]
	}
	if second.FileName != filename || second.FileID != 2 || second.UserID != userID ||
		second.LastMod != lastMod || second.FileHash != hashString || second.ChunkCount != chunkCount {
		t.Fatalf("Failed to get the added file (%s) using GetAllUserFileInfos().", filename)
	}

	// try to get the second file by ID
	fileByID, err = store.GetFileInfo(second.FileID)
	if err != nil || second.UserID != fileByID.UserID || second.FileID != fileByID.FileID ||
		second.ChunkCount != fileByID.ChunkCount || second.FileName != fileByID.FileName ||
		second.LastMod != fileByID.LastMod || second.FileHash != fileByID.FileHash {
		t.Fatalf("The file information returned %s was incorrect: %v", filename, second)
	}

	/////////////////////////////////////////////////////////////////////////////
	// File Chunk Operations
	addMissingFileChunks(t, store, &first)
	addMissingFileChunks(t, store, &second)

	// make sure no chunks are reported missing
	miaList, err := store.GetMissingChunkNumbersForFile(first.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", first.FileName, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", first.FileName)
	}

	miaList, err = store.GetMissingChunkNumbersForFile(second.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", second.FileName, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", second.FileName)
	}

	// delete the last chunk for the second file
	deleted, err := store.RemoveFileChunk(second.FileID, second.ChunkCount-1)
	if !deleted {
		t.Fatal("Failed to remove the file chunk from storage.")
	}
	if err != nil {
		t.Fatalf("Failed to remove the file chunk from storage: %v", err)
	}

	// get the MIA list again to make sure one chunk is gone
	miaList, err = store.GetMissingChunkNumbersForFile(second.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", second.FileName, err)
	}
	if len(miaList) != 1 {
		t.Fatalf("The incorrect number of missing chunks (%d) was found for the file (%s)", len(miaList), second.FileName)
	}

	// add the missing chunks again and make sure no chunks are MIA
	addMissingFileChunks(t, store, &second)
	miaList, err = store.GetMissingChunkNumbersForFile(second.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", second.FileName, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", second.FileName)
	}

	// try rebuilding the second file
	originalBytes, err := ioutil.ReadFile(second.FileName)
	if err != nil {
		t.Fatalf("Failed to read in original bytes for file %s: %v", second.FileName, err)
	}

	var frankenBytes []byte
	frankenBytes = make([]byte, 0)
	for i := 0; i < second.ChunkCount; i++ {
		fc, err := store.GetFileChunk(second.FileID, i)
		if err != nil {
			t.Fatalf("Unable to get chunk #%d for the file %s: %v", i, second.FileName, err)
		}
		frankenBytes = append(frankenBytes, fc.Chunk...)
	}

	// trim the buffer at the EOF marker of byte(0)
	eofIndex := bytes.IndexByte(frankenBytes, byte(0))
	if eofIndex < len(frankenBytes) {
		frankenBytes = frankenBytes[:eofIndex]
	}

	// do a straight comparison to see if the reconstructed file has the same bytes
	if bytes.Compare(originalBytes, frankenBytes) != 0 {
		t.Fatalf("Reconstructed file (len: %d) does not have the same bytes as the original (len: %d) .",
			len(frankenBytes), len(originalBytes))
	}

	// compare hashes
	hasher := sha1.New()
	hasher.Write(frankenBytes)
	frankenHash := hasher.Sum(nil)
	frankenHashString := base64.URLEncoding.EncodeToString(frankenHash)
	if frankenHashString != second.FileHash {
		t.Fatalf("Hash of reconstructed file doesn't match the original. %s vs %s", frankenHashString, second.FileHash)
	}
}

// split the testing process of adding a user into a separate functions so that
// it's easier to add multiple users.
func setupTestUser(store *filefreezer.Storage, user string, password string, t *testing.T) {
	// attempt to add a user
	salt, saltedPass, err := filefreezer.GenSaltedHash(password)
	if err != nil {
		t.Fatalf("Failed to generate a password hash %v", err)
	}
	success, err := store.AddUser(user, salt, saltedPass)
	if err != nil || !success {
		t.Fatalf("Failed to add a new user (%s) to storage: %v", user, err)
	}

	// verify the correct information for this user can be retrieved
	userID, userSalt, userSaltedHash, err := store.GetUser(user)
	if err != nil {
		t.Fatalf("Failed to get the user (%s ; id:%d) info from storage: %v", user, userID, err)
	}
	if userSalt != salt || bytes.Compare(userSaltedHash, saltedPass) != 0 {
		t.Fatalf("Failed to get the correct user (%s) info from storage: \n\t%s | %v\n\t%s | %v",
			user, userSalt, userSaltedHash, salt, saltedPass)
	}
	if !filefreezer.VerifyPassword(password, userSalt, userSaltedHash) {
		t.Fatalf("Password verification failed for user (%s) with stored salt and hash.", user)
	}

	// make sure password verification fails with some change to the salted hash
	bogusHash := bytes.Repeat([]byte{42}, 42)
	if filefreezer.VerifyPassword(password, userSalt, bogusHash) {
		t.Fatalf("Password verification failed for user (%s) with stored salt and hash.", user)
	}

	// set the user's quota
	err = store.SetUserQuota(userID, 1e6)
	if err != nil {
		t.Fatalf("Failed to set the user quota for %s (id:%d): %v", user, userID, err)
	}

	// now set the user quota to the right ammound
	err = store.SetUserQuota(userID, 1e9)
	if err != nil {
		t.Fatalf("Failed to update the user quota for %s (id:%d): %v", user, userID, err)
	}

	// make sure we get the correct number when we poll the quota
	userQuota, err := store.GetUserQuota(userID)
	if err != nil || userQuota != 1e9 {
		t.Fatalf("Failed to get the user quota for %s (id:%d, v:%d): %v", user, userID, userQuota, err)
	}

	// set the user's information
	err = store.SetUserInfo(userID, 0, 0)
	if err != nil {
		t.Fatalf("Failed to set the user info for %s (id:%d): %v", user, userID, err)
	}

	// test updating it
	err = store.SetUserInfo(userID, 1024, 1)
	if err != nil {
		t.Fatalf("Failed to update the user info for %s (id:%d): %v", user, userID, err)
	}

	// did the full udpate work?
	alloc, rev, err := store.GetUserInfo(userID)
	if err != nil || alloc != 1024 || rev != 1 {
		t.Fatalf("Failed to get the user info for %s (id:%d alloc:%d rev:%v): %v", user, userID, alloc, rev, err)
	}

	// try applying an allocated byte delta
	err = store.UpdateUserInfo(userID, -512)
	if err != nil {
		t.Fatalf("Failed to apply a delta to the user info for %s (id:%d): %v", user, userID, err)
	}

	// did the delta udpate work?
	alloc, rev, err = store.GetUserInfo(userID)
	if err != nil || alloc != 512 || rev != 2 {
		t.Fatalf("Failed to get the update user info for %s (id:%d alloc:%d rev:%v): %v", user, userID, alloc, rev, err)
	}

}

func calcFileHashInfo(t *testing.T, maxChunkSize int64, filename string) (chunkCount int, lastMod int64, hashString string) {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("Failed to stat the local file (%s) for the test.", filename)
	}

	lastMod = fileInfo.ModTime().Unix()

	// calculate the chunk count required for the file size
	fileSize := fileInfo.Size()
	chunkCount = int((fileSize - (fileSize % maxChunkSize) + maxChunkSize) / maxChunkSize)

	// generate a hash for the test file
	hasher := sha1.New()
	fileBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal("Failed to create a file byte array for the hashing operation.")
	}
	hasher.Write(fileBytes)
	hash := hasher.Sum(nil)
	hashString = base64.URLEncoding.EncodeToString(hash)

	return
}

func addMissingFileChunks(t *testing.T, store *filefreezer.Storage, fi *filefreezer.UserFileInfo) {
	miaList, err := store.GetMissingChunkNumbersForFile(fi.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", fi.FileName, err)
	}

	if len(miaList) != fi.ChunkCount {
		t.Fatalf("The file %s has an incorrect number of chunks missing (expected %d; got %d)",
			fi.FileName, fi.ChunkCount, len(miaList))
	}

	// loop through potential chunks, reading/adding or seeking through the file
	miaCount := len(miaList)
	buffer := make([]byte, store.ChunkSize)
	f, err := os.Open(fi.FileName)
	if err != nil {
		t.Fatalf("Failed to open the file %s: %v", fi.FileName, err)
	}
	defer f.Close()

	for i := 0; i < fi.ChunkCount; i++ {
		// if the index is found in the mia list, read and add it to the store
		if sort.SearchInts(miaList, i) < miaCount {
			_, err := io.ReadAtLeast(f, buffer, int(store.ChunkSize))
			if err != nil {
				if err == io.EOF {
					t.Fatalf("Reached EOF of the file when more chunk data was expected in file %s.", fi.FileName)
				} else if err == io.ErrUnexpectedEOF {
					// only fail the test if we haven't hit the last chunk
					if i+1 != fi.ChunkCount {
						t.Fatalf("Reached EOF while reading while not on the last chunk for file %s.", fi.FileName)
					}
				} else {
					t.Fatalf("An error occured while reading the file %s: %v", fi.FileName, err)
				}
			}

			// hash the chunk
			hasher := sha1.New()
			hasher.Write(buffer)
			hash := hasher.Sum(nil)
			chunkHash := base64.URLEncoding.EncodeToString(hash)

			// send the data to the store
			err = store.AddFileChunk(fi.FileID, i, chunkHash, buffer)
			if err != nil {
				t.Fatalf("Failed to add the chunk to storage for file %s: %v", fi.FileName, err)
			}
		}
	}
}

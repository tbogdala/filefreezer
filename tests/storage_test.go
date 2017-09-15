// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package tests

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"fmt"

	"github.com/tbogdala/filefreezer"
)

func TestMain(m *testing.M) {
	rand.Seed(time.Now().Unix())
	os.Exit(m.Run())
}

// generates a non-crypto strength random byte array of specified length
func genRandomBytes(length int) []byte {
	b := make([]byte, length)
	for i := 0; i < length; i++ {
		b[i] = byte(rand.Uint32() >> 24)
		if b[i] == 0 {
			b[i] = 1
		}
	}
	return b
}

func TestQuotasAndPermissions(t *testing.T) {
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

	// a second, legit user should be okay
	setupTestUser(store, "admin2", "hamster2", t)

	// set the user's quota to something rediculously small
	user, err := store.GetUser("admin")
	if err != nil {
		t.Fatalf("Failed to get the user: %v", err)
	}
	user2, err := store.GetUser("admin2")
	if err != nil {
		t.Fatalf("Failed to get the user: %v", err)
	}
	err = store.SetUserQuota(user.ID, 100)
	if err != nil {
		t.Fatalf("Failed to set the user quota for (id:%d): %v", user.ID, err)
	}

	// initial user cryptoHash should be initialized to a byte slice of {}
	if bytes.Compare(user.CryptoHash, []byte{}) != 0 {
		t.Fatalf("Bad default cryptoHash for the user in the database: %v", user.CryptoHash)
	}

	// test updating the crypto hash for the user
	cryptoBytes := make([]byte, 32)
	_, err = rand.Read(cryptoBytes)
	if err != nil {
		t.Fatalf("Failed to get random bytes for test: %v", err)
	}
	store.UpdateUserCryptoHash(user.ID, cryptoBytes)

	// read back the user information and make sure we updated it
	user, err = store.GetUser("admin")
	if err != nil {
		t.Fatalf("Failed to get the user: %v", err)
	}
	if bytes.Compare(user.CryptoHash, cryptoBytes) != 0 {
		t.Fatalf("Failed ot update the user's cryptoHash in the database.")
	}

	filename := "../storage.go"
	chunkCount, lastMod, permissions, hashString, err := filefreezer.CalcFileHashInfo(store.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash data (%s): %v", filename, err)
	}

	// add the file information to the storage server
	fi, err := store.AddFileInfo(user.ID, filename, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", filename, err)
	}

	// track the number of missing chunks before we fail the next test
	originalMiaList, err := store.GetMissingChunkNumbersForFile(user.ID, fi.FileID)
	if len(originalMiaList) == 0 {
		t.Fatal("Should not have an empty missing chunk list after just adding the file.")
	}

	// make sure that uploading a file fails
	err = addMissingFileChunks(store, fi)
	if err == nil {
		t.Fatal("No error was received after uploading chunks for a user with a very small quota.")
	}

	// make sure we're still missing the same number of chunks
	secondMiaList, err := store.GetMissingChunkNumbersForFile(user.ID, fi.FileID)
	if len(originalMiaList) != len(secondMiaList) {
		t.Fatal("The number of chunks missing for a file should have been the same after a failed upload.")
	}

	// reset the quota
	err = store.SetUserQuota(user.ID, 1e9)
	if err != nil {
		t.Fatalf("Failed to set the user quota for (id:%d): %v", user.ID, err)
	}

	// test to make sure you cannot upload file chunks for files not assigned to the user ID supplied
	fi.UserID = user2.ID
	err = addMissingFileChunks(store, fi)
	if err == nil {
		t.Fatal("Failed to halt a file chunk upload for chunks not belonging to the user.")
	}

	// reset the user id and upload the file chunks
	fi.UserID = user.ID
	err = addMissingFileChunks(store, fi)
	if err != nil {
		t.Fatalf("Error while uploading missing file parts for a user: %v", err)
	}

	// now attempt to delete a chunk with a bad user id
	deleted, err := store.RemoveFileChunk(user2.ID, fi.FileID, fi.CurrentVersion.VersionID, fi.CurrentVersion.ChunkCount-1)
	if deleted {
		t.Fatal("Removed the file chunk from storage with a non-owner user id")
	}
	if err == nil {
		t.Fatal("Removed the file chunk from storage with a non-owner user id")
	}
}

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
	badUser, err := store.AddUser("admin", "99999", []byte{1, 2, 3, 4, 5}, 1e9)
	if err == nil || badUser != nil {
		t.Fatal("Should have failed to add a duplicate user but did not.")
	}

	// a second, legit user should be okay
	setupTestUser(store, "admin2", "hamster2", t)

	// test to make sure we can update a user password, hash and quota
	user3, err := store.AddUser("admin4", "1234", []byte{1, 2, 3, 4}, 1e9)
	if err != nil {
		t.Fatalf("Failed to create the admin3 test user: %v", err)
	}
	err = store.UpdateUser(user3.ID, "admin3", "5678", []byte{5, 6, 7, 8}, []byte{0}, 10)
	if err != nil {
		t.Fatalf("Failed to update the user password and quota data: %v", err)
	}
	user3Test, err := store.GetUser("admin3")
	if err != nil || user3Test.ID != user3.ID || user3Test.Salt != "5678" || bytes.Compare(user3Test.SaltedHash, []byte{5, 6, 7, 8}) != 0 {
		t.Fatalf("Failed to update the user's password and hash.")
	}
	user3Stats, err := store.GetUserStats(user3.ID)
	if err != nil || user3Stats.Quota != 10 {
		t.Fatalf("Failed to update the user's quota: %v", err)
	}

	// these calls for made up users should fail
	badUser, err = store.GetUser("ghost")
	if err == nil || badUser != nil {
		t.Fatal("GetUser succeeded with a user that shouldn't exist in the database.")
	}
	badInfo, err := store.GetUserStats(777)
	if err == nil || badInfo != nil {
		t.Fatal("GetUserStats succeeded with a user that shouldn't exist in the database.")
	}
	err = store.UpdateUserStats(777, 512)
	if err == nil {
		t.Fatal("UpdateUserStats succeeded with a user that shouldn't exist in the database.")
	}

	///////////////////////////////////////////////////////////////////////////
	// File manipulation

	// get user credentials
	username := "admin"
	user, err := store.GetUser(username)
	if err != nil {
		t.Fatal("GetUser failed to get the admin test user.")
	}

	// pull up the local file information
	filename := "../README.md"
	chunkCount, lastMod, permissions, hashString, err := filefreezer.CalcFileHashInfo(store.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", filename, err)
	}

	// add the file information to the storage server
	fi, err := store.AddFileInfo(user.ID, filename, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", filename, err)
	}

	// make sure we can get it by name
	_, err = store.GetFileInfoByName(user.ID, filename)
	if err != nil {
		t.Fatalf("Failed to access a file by name: %v", err)
	}

	// now test removing it
	err = store.RemoveFileInfo(fi.FileID)
	if err != nil {
		t.Fatalf("Failed to remove the file we just added.")
	}
	fileInfos, err := store.GetAllUserFileInfos(user.ID)
	if err != nil {
		t.Fatalf("Failed to get all of the user (id:%d) file infos in storage: %v", user.ID, err)
	}
	if len(fileInfos) != 0 {
		t.Fatalf("Returned the wrong number of file infos (%d) for a user (id:%d).", len(fileInfos), user.ID)
	}

	// add the file information to the storage server again for the rest of the tests
	_, err = store.AddFileInfo(user.ID, filename, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", filename, err)
	}

	// get all the file info objects
	fileInfos, err = store.GetAllUserFileInfos(user.ID)
	if err != nil {
		t.Fatalf("Failed to get all of the user (id:%d) file infos in storage: %v", user.ID, err)
	}
	if len(fileInfos) != 1 {
		t.Fatalf("Returned the wrong number of file infos (%d) for a user (id:%d).", len(fileInfos), user.ID)
	}

	var first, second *filefreezer.FileInfo
	first = &fileInfos[0]
	if first.UserID != user.ID || first.FileID != 1 || first.FileName != filename ||
		first.CurrentVersion.ChunkCount != chunkCount || first.CurrentVersion.LastMod != lastMod ||
		first.CurrentVersion.FileHash != hashString || first.IsDir != false ||
		first.CurrentVersion.Permissions != permissions {
		t.Fatalf("The file information returned %s was incorrect: %v", filename, first)
	}

	// try to get the file again, but by ID
	fileByID, err := store.GetFileInfo(first.UserID, first.FileID)
	if err != nil || first.UserID != fileByID.UserID || first.FileID != fileByID.FileID ||
		first.CurrentVersion.ChunkCount != fileByID.CurrentVersion.ChunkCount || first.FileName != fileByID.FileName ||
		first.CurrentVersion.LastMod != fileByID.CurrentVersion.LastMod ||
		first.CurrentVersion.FileHash != fileByID.CurrentVersion.FileHash ||
		first.IsDir != fileByID.IsDir || first.CurrentVersion.Permissions != fileByID.CurrentVersion.Permissions {
		t.Fatalf("Failed to get the added file using the fileID (%d) returned by GetAllUserFileInfos(): %v", first.FileID, err)
	}

	// try getting a bad file id
	_, err = store.GetFileInfo(first.UserID, 777)
	if err == nil {
		t.Fatal("Getting a user file info object for a non-existant fileID succeeded when a failure was expected.")
	}

	// try getting with a bad user id
	_, err = store.GetFileInfo(42, first.FileID)
	if err == nil {
		t.Fatal("Getting a user file info with a non-matching user ID succeeded when a failure was expected.")
	}

	// add a second file
	filename = "../storage.go"
	chunkCount, lastMod, permissions, hashString, err = filefreezer.CalcFileHashInfo(store.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", filename, err)
	}

	// add the file information to the storage server
	_, err = store.AddFileInfo(user.ID, filename, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", filename, err)
	}

	// attempt to add the same file information again, which should fail as a duplicate
	_, err = store.AddFileInfo(user.ID, filename, false, permissions, lastMod, chunkCount, hashString)
	if err == nil {
		t.Fatal("Added a duplicate filename under the same user successuflly when a failure was expected.")
	}

	// get all the file info objects again
	fileInfos, err = store.GetAllUserFileInfos(user.ID)
	if err != nil {
		t.Fatalf("Failed to get all of the user (id:%d) file infos in storage: %v", user.ID, err)
	}
	if len(fileInfos) != 2 {
		t.Fatalf("Returned the wrong number of file infos (%d) for a user (id:%d).", len(fileInfos), user.ID)
	}
	first = &fileInfos[0]
	second = &fileInfos[1]
	if second.FileName != filename {
		first = &fileInfos[1]
		second = &fileInfos[0]
	}
	if second.FileName != filename || second.FileID != 2 || second.UserID != user.ID ||
		second.CurrentVersion.LastMod != lastMod || second.CurrentVersion.FileHash != hashString ||
		second.CurrentVersion.ChunkCount != chunkCount {
		t.Fatalf("Failed to get the added file (%s) using GetAllUserFileInfos().", filename)
	}

	// try to get the second file by ID
	fileByID, err = store.GetFileInfo(second.UserID, second.FileID)
	if err != nil || second.UserID != fileByID.UserID || second.FileID != fileByID.FileID ||
		second.CurrentVersion.ChunkCount != fileByID.CurrentVersion.ChunkCount || second.FileName != fileByID.FileName ||
		second.CurrentVersion.LastMod != fileByID.CurrentVersion.LastMod ||
		second.CurrentVersion.FileHash != fileByID.CurrentVersion.FileHash {
		t.Fatalf("The file information returned %s was incorrect: %v", filename, second)
	}

	/////////////////////////////////////////////////////////////////////////////
	// File Chunk Operations
	err = addMissingFileChunks(store, first)
	if err != nil {
		t.Fatalf("Failed to add file chunks: %v", err)
	}

	// pull the stats before removing the file to make sure the allocation
	// count changes correctly
	userStats, err := store.GetUserStats(first.UserID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for user id %d: %v", first.UserID, err)
	}

	// now test wiping out the file completely
	err = store.RemoveFile(first.UserID, first.FileID)
	if err != nil {
		t.Fatalf("Failed to remove the file and all of the file chunks: %v", err)
	}

	// check to make sure the allocated count changed
	oldAllocation := userStats.Allocated
	userStats, err = store.GetUserStats(first.UserID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for user id %d: %v", first.UserID, err)
	}
	if userStats.Allocated >= oldAllocation {
		t.Fatalf("The allocation count did not go down after removing a file (pre: %d ; post: %d).", oldAllocation, userStats.Allocated)
	}

	// make sure we can't get the file chunk or the file info
	_, err = store.GetFileChunk(first.FileID, first.CurrentVersion.VersionID, 0)
	if err == nil {
		t.Fatalf("Got the first file chunk for the first file when failure was expected.")
	}
	_, err = store.GetFileInfo(first.UserID, first.FileID)
	if err == nil {
		t.Fatalf("Got the file info structure for the file that was just deleted when failure was expected.")
	}

	// add the first file back in so that the rests of the tests can continue
	first, err = store.AddFileInfo(first.UserID, first.FileName, first.IsDir, first.CurrentVersion.Permissions,
		first.CurrentVersion.LastMod, first.CurrentVersion.ChunkCount, first.CurrentVersion.FileHash)
	if err != nil {
		t.Fatalf("Failed to add a the file again (%s): %v", first.FileName, err)
	}
	err = addMissingFileChunks(store, first)
	if err != nil {
		t.Fatalf("Failed to add first file chunks: %v", err)
	}

	err = addMissingFileChunks(store, second)
	if err != nil {
		t.Fatalf("Failed to add file chunks: %v", err)
	}

	// make sure no chunks are reported missing
	miaList, err := store.GetMissingChunkNumbersForFile(first.UserID, first.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", first.FileName, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", first.FileName)
	}

	// see that we can get the file chunk infos for the file
	firstFileChunks, err := store.GetFileChunkInfos(first.UserID, first.FileID, first.CurrentVersion.VersionID)
	if err != nil {
		t.Fatalf("Could not get a list of file chunks for the file (%s): %v:", first.FileName, err)
	}
	if len(firstFileChunks) != first.CurrentVersion.ChunkCount {
		t.Fatalf("Returned list of file chunks for a file didn't match the expected count. (got: %d, expected %d)",
			len(firstFileChunks), first.CurrentVersion.ChunkCount)
	}
	// check to see if some of the chunk data is correct
	chunk, err := store.GetFileChunk(first.FileID, 0, first.CurrentVersion.VersionID)
	if err != nil {
		t.Fatalf("Failed to get the first file chunk for the first file: %v", err)
	}
	if chunk.ChunkHash != firstFileChunks[0].ChunkHash || firstFileChunks[0].ChunkNumber != 0 {
		t.Fatalf("Chunk info doesn't match up between Storage.GetFileChunk() and Storage.GetFileChunkInfos()")
	}

	// test for failure when requesting missing chunks with an incorrect user id
	_, err = store.GetMissingChunkNumbersForFile(42, first.FileID)
	if err == nil {
		t.Fatal("A call to GetMissingChunkNumbersForFile with an incorrect user ID succeeded when failure was expected.")
	}

	miaList, err = store.GetMissingChunkNumbersForFile(second.UserID, second.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", second.FileName, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", second.FileName)
	}

	beforeDeleteStats, err := store.GetUserStats(second.UserID)
	if err != nil {
		t.Fatalf("Failed to get the user's allocation and revision count: %v", err)
	}

	// delete the last chunk for the second file
	deleted, err := store.RemoveFileChunk(second.UserID, second.FileID, second.CurrentVersion.VersionID, second.CurrentVersion.ChunkCount-1)
	if !deleted {
		t.Fatal("Failed to remove the file chunk from storage.")
	}
	if err != nil {
		t.Fatalf("Failed to remove the file chunk from storage: %v", err)
	}

	afterDeleteStats, err := store.GetUserStats(second.UserID)
	if err != nil {
		t.Fatalf("Failed to get the user's allocation and revision count: %v", err)
	}

	// make sure the rev count incremented and allocation count decreased appropriately
	if beforeDeleteStats.Allocated == afterDeleteStats.Allocated ||
		afterDeleteStats.Revision-beforeDeleteStats.Revision != 1 {
		t.Fatalf("Failed to update the user's allocation count and revision count after deleting a chunk.")
	}

	// get the MIA list again to make sure one chunk is gone
	miaList, err = store.GetMissingChunkNumbersForFile(second.UserID, second.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", second.FileName, err)
	}
	if len(miaList) != 1 {
		t.Fatalf("The incorrect number of missing chunks (%d) was found for the file (%s)", len(miaList), second.FileName)
	}

	// add the missing chunks again and make sure no chunks are MIA
	err = addMissingFileChunks(store, second)
	if err != nil {
		t.Fatalf("Failed to upload missing file chunks after one was deleted: %v", err)
	}

	miaList, err = store.GetMissingChunkNumbersForFile(second.UserID, second.FileID)
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
	for i := 0; i < second.CurrentVersion.ChunkCount; i++ {
		fc, err := store.GetFileChunk(second.FileID, i, second.CurrentVersion.VersionID)
		if err != nil {
			t.Fatalf("Unable to get chunk #%d for the file %s: %v", i, second.FileName, err)
		}
		frankenBytes = append(frankenBytes, fc.Chunk...)
	}

	// trim the buffer at the EOF marker of byte(0)
	eofIndex := bytes.IndexByte(frankenBytes, byte(0))
	if eofIndex > 0 && eofIndex < len(frankenBytes) {
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
	if frankenHashString != second.CurrentVersion.FileHash {
		t.Fatalf("Hash of reconstructed file doesn't match the original. %s vs %s", frankenHashString, second.CurrentVersion.FileHash)
	}

	// make sure we got files still bound to the user
	allFiles, err := store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) < 1 {
		t.Fatalf("Not enough files left for the user to perform the next test.") // sanity test of the test
	}

	// attempt to remove the user as a final test
	err = store.RemoveUser(user.Name)
	if err != nil {
		t.Fatalf("Failed to remove the user %s: %v", user.Name, err)
	}

	// we should now not be able to get user information
	allFiles, err = store.GetAllUserFileInfos(user.ID)
	if err != nil {
		t.Fatalf("Getting file infos for a user that doesn't exist failed with an error: %v", err)
	}
	if len(allFiles) > 0 {
		t.Fatalf("There were files left behind after removing a user.")
	}
}

func TestFileVersioning(t *testing.T) {
	bytesAllocated := 0

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
	setupTestUser(store, "admin", "12345667890", t)
	user, err := store.GetUser("admin")
	if err != nil {
		t.Fatal("GetUser failed to get the admin test user.")
	}

	// make sure we don't have any files in the database already as a sanity check
	allFiles, err := store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) > 0 {
		t.Fatalf("The server isn't in a clean state and already has file associated with the user.")
	}

	// generate some test file data
	const testFilename1 = "versioning_test_001.dat"
	rando1 := genRandomBytes(int(store.ChunkSize) * 3)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	defer os.Remove(testFilename1)

	// get the local file information
	chunkCount, lastMod, permissions, hashString, err := filefreezer.CalcFileHashInfo(store.ChunkSize, testFilename1)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", testFilename1, err)
	}

	///////////////////////////////////////////////////////////////////////////
	// upload initial version

	// add the file information to the storage server
	fi, err := store.AddFileInfo(user.ID, testFilename1, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to add a new file (%s): %v", testFilename1, err)
	}

	// upload the original version of the file
	err = addMissingFileChunks(store, fi)
	if err != nil {
		t.Fatalf("An error was received after uploading chunks of a test file: %v", err)
	}

	// verify we have the file registered
	allFiles, err = store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) != 1 {
		t.Fatalf("The server didn't register the uploading of the test file correctly.")
	}

	// make sure all chunks have been uploaded
	miaList, err := store.GetMissingChunkNumbersForFile(user.ID, fi.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", testFilename1, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", testFilename1)
	}

	// make sure there's only one file version regiestered for the file
	versionIDs, versionNums, err := store.GetFileVersions(fi.FileID)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versionIDs) != 1 || len(versionNums) != 1 {
		t.Fatalf("Expected to get one file version for the test file but received %d.", len(versionIDs))
	}
	if versionNums[0] != 1 {
		t.Fatalf("The first version number for the test file was not 1, it was %d.", versionNums[0])
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1)
	userStats, err := store.GetUserStats(user.ID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// modify existing chunk and upload a new version
	rando1[0] = 0xDE
	rando1[1] = 0xAD
	rando1[2] = 0xBE
	rando1[3] = 0xEF
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	chunkCount, lastMod, permissions, hashString, err = filefreezer.CalcFileHashInfo(store.ChunkSize, testFilename1)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", testFilename1, err)
	}

	// register a new version of the file in storage with the updated local information
	fiV2, err := store.TagNewFileVersion(user.ID, fi.FileID, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to tag a new file version for %s: %v", testFilename1, err)
	}
	if fiV2.FileID != fi.FileID {
		t.Fatalf("File ID changed between version tagging from %d to %d.", fi.FileID, fiV2.FileID)
	}

	// upload the second version of the file
	err = addMissingFileChunks(store, fiV2)
	if err != nil {
		t.Fatalf("An error was received after uploading chunks of the test file v2: %v", err)
	}

	// verify we still only have the one file registered
	allFiles, err = store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) != 1 {
		t.Fatalf("The server has more tha one file registered to a user when uploading the second version.")
	}

	// make sure all chunks have been uploaded
	miaList, err = store.GetMissingChunkNumbersForFile(user.ID, fiV2.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", testFilename1, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", testFilename1)
	}

	// verify that we get two versions back for the given file ID
	versionIDs, versionNums, err = store.GetFileVersions(fiV2.FileID)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versionIDs) != 2 || len(versionNums) != 2 {
		t.Fatalf("Expected to get two file versions for the test file but received %d.", len(versionIDs))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1)
	userStats, err = store.GetUserStats(user.ID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// modify all existing chunks and upload a new version
	rando1 = genRandomBytes(int(store.ChunkSize) * 3)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	chunkCount, lastMod, permissions, hashString, err = filefreezer.CalcFileHashInfo(store.ChunkSize, testFilename1)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", testFilename1, err)
	}

	// register a new version of the file in storage with the updated local information
	fiV3, err := store.TagNewFileVersion(user.ID, fiV2.FileID, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to tag a new file version for %s: %v", testFilename1, err)
	}
	if fiV3.FileID != fi.FileID {
		t.Fatalf("File ID changed between version tagging from %d to %d.", fi.FileID, fiV3.FileID)
	}

	// upload the third version of the file
	err = addMissingFileChunks(store, fiV3)
	if err != nil {
		t.Fatalf("An error was received after uploading chunks of the test file v3: %v", err)
	}

	// verify we still only have the one file registered
	allFiles, err = store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) != 1 {
		t.Fatalf("The server has more tha one file registered to a user when uploading the third version.")
	}

	// make sure all chunks have been uploaded
	miaList, err = store.GetMissingChunkNumbersForFile(user.ID, fiV3.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", testFilename1, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", testFilename1)
	}

	// verify that we get three versions back for the given file ID
	versionIDs, versionNums, err = store.GetFileVersions(fiV3.FileID)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versionIDs) != 3 || len(versionNums) != 3 {
		t.Fatalf("Expected to get three file versions for the test file but received %d.", len(versionIDs))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1)
	userStats, err = store.GetUserStats(user.ID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// make a larger file and upload a new version
	rando1 = genRandomBytes(int(store.ChunkSize) * 6)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	chunkCount, lastMod, permissions, hashString, err = filefreezer.CalcFileHashInfo(store.ChunkSize, testFilename1)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", testFilename1, err)
	}

	// register a new version of the file in storage with the updated local information
	fiV4, err := store.TagNewFileVersion(user.ID, fiV3.FileID, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to tag a new file version for %s: %v", testFilename1, err)
	}
	if fiV4.FileID != fi.FileID {
		t.Fatalf("File ID changed between version tagging from %d to %d.", fi.FileID, fiV4.FileID)
	}

	// upload the fourth version of the file
	err = addMissingFileChunks(store, fiV4)
	if err != nil {
		t.Fatalf("An error was received after uploading chunks of the test file v4: %v", err)
	}

	// verify we still only have the one file registered
	allFiles, err = store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) != 1 {
		t.Fatalf("The server has more tha one file registered to a user when uploading the fourth version.")
	}

	// make sure all chunks have been uploaded
	miaList, err = store.GetMissingChunkNumbersForFile(user.ID, fiV4.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", testFilename1, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", testFilename1)
	}

	// verify that we get four versions back for the given file ID
	versionIDs, versionNums, err = store.GetFileVersions(fiV4.FileID)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versionIDs) != 4 || len(versionNums) != 4 {
		t.Fatalf("Expected to get four versions for the test file but received %d.", len(versionIDs))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1)
	userStats, err = store.GetUserStats(user.ID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// make the file smaller and upload a new version
	rando1 = rando1[:(int(store.ChunkSize)*2)-1]
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	chunkCount, lastMod, permissions, hashString, err = filefreezer.CalcFileHashInfo(store.ChunkSize, testFilename1)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", testFilename1, err)
	}

	// register a new version of the file in storage with the updated local information
	fiV5, err := store.TagNewFileVersion(user.ID, fiV4.FileID, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to tag a new file version for %s: %v", testFilename1, err)
	}
	if fiV5.FileID != fi.FileID {
		t.Fatalf("File ID changed between version tagging from %d to %d.", fi.FileID, fiV4.FileID)
	}

	// upload the fifth version of the file
	err = addMissingFileChunks(store, fiV5)
	if err != nil {
		t.Fatalf("An error was received after uploading chunks of the test file v5: %v", err)
	}

	// verify we still only have the one file registered
	allFiles, err = store.GetAllUserFileInfos(user.ID)
	if err != nil || len(allFiles) != 1 {
		t.Fatalf("The server has more tha one file registered to a user when uploading the fifth version.")
	}

	// make sure all chunks have been uploaded
	miaList, err = store.GetMissingChunkNumbersForFile(user.ID, fiV5.FileID)
	if err != nil {
		t.Fatalf("Could not get a list of missing chunks for the file (%s): %v", testFilename1, err)
	}
	if len(miaList) != 0 {
		t.Fatalf("Missing chunks were found for the file (%s) after uploading them.", testFilename1)
	}

	// verify that we get five versions back for the given file ID
	versionIDs, versionNums, err = store.GetFileVersions(fiV5.FileID)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versionIDs) != 5 || len(versionNums) != 5 {
		t.Fatalf("Expected to get five file versions for the test file but received %d.", len(versionIDs))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1)
	userStats, err = store.GetUserStats(user.ID)
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}
}

// split the testing process of adding a user into a separate functions so that
// it's easier to add multiple users.
func setupTestUser(store *filefreezer.Storage, username string, password string, t *testing.T) {
	// attempt to add a user
	salt, saltedPass, err := filefreezer.GenLoginPasswordHash(password)
	if err != nil {
		t.Fatalf("Failed to generate a password hash %v", err)
	}
	user, err := store.AddUser(username, salt, saltedPass, 1e9)
	if err != nil || user == nil {
		t.Fatalf("Failed to add a new user (%s) to storage: %v", username, err)
	}

	// verify the correct information for this user can be retrieved
	userDupe, err := store.GetUser(user.Name)
	if err != nil {
		t.Fatalf("Failed to get the user (%s ; id:%d) info from storage: %v", user.Name, user.ID, err)
	}
	if userDupe.Salt != salt || bytes.Compare(userDupe.SaltedHash, saltedPass) != 0 {
		t.Fatalf("Failed to get the correct user (%s) info from storage: \n\t%s | %v\n\t%s | %v",
			username, userDupe.Salt, userDupe.SaltedHash, salt, saltedPass)
	}
	if !filefreezer.VerifyLoginPassword(password, userDupe.Salt, userDupe.SaltedHash) {
		t.Fatalf("Password verification failed for user (%s) with stored salt and hash.", username)
	}

	// make sure password verification fails with some change to the salted hash
	bogusHash := bytes.Repeat([]byte{42}, 42)
	if filefreezer.VerifyLoginPassword(password, userDupe.Salt, bogusHash) {
		t.Fatalf("Password verification failed for user (%s) with stored salt and hash.", username)
	}

	// set the user's information
	err = store.SetUserStats(user.ID, 1e9, 0, 0)
	if err != nil {
		t.Fatalf("Failed to set the user info for %s (id:%d): %v", username, user.ID, err)
	}

	// set the user's quota
	err = store.SetUserQuota(user.ID, 1e6)
	if err != nil {
		t.Fatalf("Failed to set the user quota for %s (id:%d): %v", username, user.ID, err)
	}

	// now set the user quota to the right ammound
	err = store.SetUserQuota(user.ID, 1e9)
	if err != nil {
		t.Fatalf("Failed to update the user quota for %s (id:%d): %v", username, user.ID, err)
	}

	// make sure we get the correct number when we poll the quota
	userStats, err := store.GetUserStats(user.ID)
	if err != nil || userStats == nil || userStats.Quota != 1e9 {
		t.Fatalf("Failed to get the user quota for %s (id:%d, v:%v): %v", username, user.ID, userStats, err)
	}

	// test updating it
	err = store.SetUserStats(user.ID, 1e9, 1024, 1)
	if err != nil {
		t.Fatalf("Failed to update the user info for %s (id:%d): %v", username, user.ID, err)
	}

	// did the full udpate work?
	userStats, err = store.GetUserStats(user.ID)
	if err != nil || userStats.Allocated != 1024 || userStats.Revision != 1 || userStats.Quota != 1e9 {
		t.Fatalf("Failed to get the user info for %s (id:%d alloc:%d rev:%v): %v", username, user.ID,
			userStats.Allocated, userStats.Revision, err)
	}

	// try applying an allocated byte delta
	err = store.UpdateUserStats(user.ID, -1024)
	if err != nil {
		t.Fatalf("Failed to apply a delta to the user info for %s (id:%d): %v", username, user.ID, err)
	}

	// did the delta udpate work?
	userStats, err = store.GetUserStats(user.ID)
	if err != nil || userStats.Allocated != 0 || userStats.Revision != 2 || userStats.Quota != 1e9 {
		t.Fatalf("Failed to get the user info for %s (id:%d alloc:%d rev:%v): %v", username, user.ID,
			userStats.Allocated, userStats.Revision, err)
	}
}

func addMissingFileChunks(store *filefreezer.Storage, fi *filefreezer.FileInfo) error {
	miaList, err := store.GetMissingChunkNumbersForFile(fi.UserID, fi.FileID)
	if err != nil {
		return fmt.Errorf("Could not get a list of missing chunks for the file (%s): %v", fi.FileName, err)
	}

	if len(miaList) != fi.CurrentVersion.ChunkCount {
		return fmt.Errorf("The file %s has an incorrect number of chunks missing (expected %d; got %d)",
			fi.FileName, fi.CurrentVersion.ChunkCount, len(miaList))
	}

	// loop through potential chunks, reading/adding or seeking through the file
	miaCount := len(miaList)
	buffer := make([]byte, store.ChunkSize)
	f, err := os.Open(fi.FileName)
	if err != nil {
		return fmt.Errorf("Failed to open the file %s: %v", fi.FileName, err)
	}
	defer f.Close()

	for i := 0; i < fi.CurrentVersion.ChunkCount; i++ {
		// if the index is found in the mia list, read and add it to the store
		if sort.SearchInts(miaList, i) < miaCount {
			readCount, err := io.ReadAtLeast(f, buffer, int(store.ChunkSize))
			if err != nil {
				if err == io.EOF {
					return fmt.Errorf("reached EOF of the file when more chunk data was expected in file %s", fi.FileName)
				} else if err == io.ErrUnexpectedEOF {
					// only fail the test if we haven't hit the last chunk
					if i+1 != fi.CurrentVersion.ChunkCount {
						return fmt.Errorf("reached EOF while reading while not on the last chunk for file %s", fi.FileName)
					}
				} else {
					return fmt.Errorf("An error occured while reading the file %s: %v", fi.FileName, err)
				}
			}

			clampedBuffer := buffer[:readCount]

			// hash the chunk
			hasher := sha1.New()
			hasher.Write(clampedBuffer)
			hash := hasher.Sum(nil)
			chunkHash := base64.URLEncoding.EncodeToString(hash)

			// check the allocation and revision count
			start, err := store.GetUserStats(fi.UserID)
			if err != nil {
				return fmt.Errorf("Failed to get the starting allocation and revision count: %v", err)
			}

			// send the data to the store
			newChunk, err := store.AddFileChunk(fi.UserID, fi.FileID, fi.CurrentVersion.VersionID, i, chunkHash, clampedBuffer)
			if err != nil {
				return fmt.Errorf("Failed to add the chunk to storage for file %s: %v", fi.FileName, err)
			}

			// make sure the new object has the correct values
			if newChunk == nil || newChunk.FileID != fi.FileID || newChunk.ChunkNumber != i ||
				newChunk.ChunkHash != chunkHash || bytes.Compare(newChunk.Chunk, clampedBuffer) != 0 {
				return fmt.Errorf("new chunk object returned from Storage.AddFileChunk had incorrect field values (%d, %d, %s) vs (%d, %d, %s)",
					fi.FileID, i, chunkHash, newChunk.FileID, newChunk.ChunkNumber, newChunk.ChunkHash)
			}

			// check the allocation and revision count
			end, err := store.GetUserStats(fi.UserID)
			if err != nil {
				return fmt.Errorf("Failed to get the ending allocation and revision count: %v", err)
			}

			// this should hold true because this database isn't getting hit by other
			// requests which could update this between transactions.
			if end.Allocated-start.Allocated != len(clampedBuffer) && end.Revision-start.Revision == 1 {
				return fmt.Errorf("Failed to update the user allocation (%d -> %d) and rev count (%d -> %d) for byte count %d",
					start.Allocated, end.Allocated, start.Revision, end.Revision, len(clampedBuffer))
			}
		}
	}

	return nil
}

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"testing"
	"time"

	"io/ioutil"

	"bytes"

	"strings"

	"github.com/spf13/afero"
	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/command"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

const (
	useHTTPS       = false
	testServerAddr = ":8080"
	testDataDir    = "testdata"
	testDataDir2   = "testdata/subdir"
	testDataDir3   = "testdata/empty"
	testFilename1  = "testdata/unit_test_1.dat"
	testFilename2  = "testdata/unit_test_2.dat"
	testFilename3  = "testdata/subdir/unit_test_3.dat"
	testFilename4  = "testdata/unit_test_empty.dat"
)

var (
	state    *serverState
	testHost string
	AppFs    = afero.NewOsFs()
)

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

// set the flags up to use the certificates used for testing via https and TLS
func setupHTTPSTestFlags() {
	*flagTLSKey = "freezer.key"
	*flagTLSCrt = "freezer.crt"
	testHost = "https://127.0.0.1:8080"
}

// set the flags up to use plain http
func setupHTTPTestFlags() {
	*flagTLSKey = ""
	*flagTLSCrt = ""
	testHost = "http://127.0.0.1:8080"
}

func TestMain(m *testing.M) {
	// instead of using command line flags for the unit test, we'll just
	// override the flag values right here
	*flagDatabasePath = "file::memory:?mode=memory&cache=shared"
	*flagServeChunkSize = 1024 * 1024 * 4
	*flagExtraStrict = true
	*argServeListenAddr = testServerAddr
	*flagCryptoPass = "beavers_and_ducks"

	if useHTTPS {
		setupHTTPSTestFlags()
	} else {
		setupHTTPTestFlags()
	}

	// make sure the test data folder exists
	os.RemoveAll(testDataDir2)
	os.MkdirAll(testDataDir2, os.ModeDir|os.ModePerm)

	// write out some random files
	rand.Seed(time.Now().Unix())
	rando1 := genRandomBytes(int(*flagServeChunkSize) * 3)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	rando2 := genRandomBytes(int(*flagServeChunkSize)*2 + 42)
	ioutil.WriteFile(testFilename2, rando2, os.ModePerm)
	rando3 := genRandomBytes(int(*flagServeChunkSize) * 4)
	ioutil.WriteFile(testFilename3, rando3, os.ModePerm)

	// run a new state in a server
	var err error
	state, err = newState()
	if err != nil {
		log.Fatalf("Unable to initialize the server: %v", err)
	}
	defer state.close()

	// this new server will run in a separate goroutine
	readyCh := make(chan bool)
	go state.serve(readyCh)

	<-readyCh
	os.Exit(m.Run())
}

func TestEverything(t *testing.T) {
	cmdState := command.NewState()

	// create a test user
	username := "admin"
	password := "1234"
	userQuota := int(1e9)
	user := cmdState.AddUser(state.Storage, username, password, userQuota)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	err := cmdState.Authenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}
	if cmdState.ServerCapabilities.ChunkSize != *flagServeChunkSize {
		t.Fatalf("Server capabilities returned a different chunk size than configured for the test: %d", *flagServeChunkSize)
	}

	err = cmdState.SetCryptoHashForPassword(*flagCryptoPass)
	if err != nil {
		t.Fatalf("Failed to set the crypto password for the test user: %v", err)
	}
	cmdState.CryptoKey, err = filefreezer.VerifyCryptoPassword(*flagCryptoPass, string(cmdState.CryptoHash))
	if err != nil {
		t.Fatalf("Failed to set the crypto key for the test user: %v", err)
	}

	// getting the user stats now should have default quota and otherwise empty settings
	userStats, err := cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Quota != userQuota {
		t.Fatalf("Got the wrong quota for the authenticated user: %d", userStats.Quota)
	}
	if userStats.Allocated != 0 {
		t.Fatalf("Got the wrong allocation count for the authenticated user: %d", userStats.Allocated)
	}

	// pull all the file infos ... should be empty
	allFiles, err := cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 0 {
		t.Fatalf("Expected to get an empty slice of FileInfo, but instead got one of length %d.", len(allFiles))
	}
	t.Logf("Got all of the file names (%d) ...", len(allFiles))

	// the revision should not have changed by only getting the file hashes
	oldRevision := userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// test adding a file
	filename := testFilename1
	fileStats, err := filefreezer.CalcFileHashInfo(cmdState.ServerCapabilities.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", filename, err)
	}
	t.Logf("Calculated hash data for %s ...", filename)

	syncStatus, ulCount, err := cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to at the file %s: %v", filename, err)
	}
	if syncStatus != command.SyncStatusLocalNewer {
		t.Fatalf("Synced local file was not newer: %s", filename)
	}
	if ulCount != fileStats.ChunkCount {
		t.Fatalf("Sync of local file didn't sync the expected number (%d) of chunks: got %d.", fileStats.ChunkCount, ulCount)
	}

	// at this point we should have a different revision
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// now that the file is registered, sync the data
	syncStatus, ulCount, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the file %s to the server: %v", filename, err)
	}
	if syncStatus != command.SyncStatusSame {
		t.Fatalf("Initial sync after add should be identical for file %s", filename)
	}
	if ulCount != 0 {
		t.Fatalf("The first sync of the first test file should be identical, but sync said %d chunks were uploaded.", ulCount)
	}

	// at this point we should have the same revision because the file was unchanged
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// now we get a chunk list for the file
	fileInfo, err := cmdState.GetFileInfoByFilename(filename)
	if err != nil {
		t.Fatalf("Failed to get the local file's information from the server: %v", err)
	}

	var remoteChunks models.FileChunksGetResponse
	target := fmt.Sprintf("%s/api/chunk/%d/%d", cmdState.HostURI, fileInfo.FileID, fileInfo.CurrentVersion.VersionID)
	body, err := cmdState.RunAuthRequest(target, "GET", cmdState.AuthToken, nil)
	err = json.Unmarshal(body, &remoteChunks)
	if err != nil {
		t.Fatalf("Failed to get the file chunk list for the file name given (%s): %v", filename, err)
	}
	if len(remoteChunks.Chunks) != fileStats.ChunkCount {
		t.Fatalf("The synced file %s doesn't have the correct number of chunks on the server (got:%d expected:%d). %v",
			filename, len(remoteChunks.Chunks), fileStats.ChunkCount, remoteChunks)
	}

	// at this point we should have the same revision
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// sleep a second then regenerate the file
	time.Sleep(time.Second)
	rando1 := genRandomBytes(int(*flagServeChunkSize * 3))
	ioutil.WriteFile(filename, rando1, os.ModePerm)

	// now that the file is regenerated, sync the data
	syncStatus, ulCount, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the file %s to the server: %v", filename, err)
	}
	if syncStatus != command.SyncStatusLocalNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if ulCount != 3 {
		t.Fatalf("The first sync of the changed test file should have uploaded 3 chunks but it uploaded %d.", ulCount)
	}

	// set the old revision count here to test below and make sure that
	// allocation counts stayed the same since the file synced above is the same size
	oldAllocation := userStats.Allocated
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Allocated == oldAllocation {
		t.Fatalf("Allocation counts should change for syncing a file of the same size but a different version.")
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("Revision should have changed after regenerating a file.")
	}
	oldRevision = userStats.Revision

	// read the local file into a byte array for test purposes
	originalTestFile, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Couldn't read local test file %s: %v", filename, err)
	}

	// delete the local file and run sync again to download
	err = os.Remove(filename)
	if err != nil {
		t.Fatalf("Failed to delete the local test file %s: %v", filename, err)
	}
	syncStatus, dlCount, err := cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != command.SyncStatusRemoteNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if dlCount != 3 {
		t.Fatalf("The sync of the changed test file should have downloaded 3 chunks but it downloaded %d.", dlCount)
	}

	// read in the downloaded file and then compare the bytes
	downloadedTestFile, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Couldn't read local test file %s: %v", filename, err)
	}
	if bytes.Compare(originalTestFile, downloadedTestFile) != 0 {
		t.Fatalf("The sync of file %s failed to download an identical copy.", filename)
	}

	// at this point we should have the same revision
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// generate some new test bytes
	frankenBytes := genRandomBytes(int(*flagServeChunkSize) * 3)
	err = ioutil.WriteFile(filename, frankenBytes, os.ModePerm)
	if err != nil {
		t.Fatalf("Couldn't write original bytes back out to the file %s: %v", filename, err)
	}

	// set the modified and access times back 2 minutes
	testTime := time.Now().Add(time.Second * -120)
	err = AppFs.Chtimes(filename, testTime, testTime)
	if err != nil {
		t.Fatalf("Couldn't set the filesystem times for the test file %s: %v", filename, err)
	}

	// syncing again should pull a new copy down
	syncStatus, dlCount, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != command.SyncStatusRemoteNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if dlCount != 3 {
		t.Fatalf("The sync of the changed test file should have downloaded 3 chunks but it downloaded %d.", dlCount)
	}

	// read in the downloaded file and then compare the bytes
	downloadedTestFile, err = ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Couldn't read local test file %s: %v", filename, err)
	}
	if bytes.Compare(originalTestFile, downloadedTestFile) != 0 {
		t.Fatalf("The sync of file %s failed to download an identical copy.", filename)
	}

	// at this point we should have the same revision
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// test syncing a file not registered on the server
	filename = testFilename2
	syncStatus, ulCount, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != command.SyncStatusLocalNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if ulCount != 3 {
		t.Fatalf("The sync of the changed test file should have downloaded 3 chunks but it downloaded %d.", dlCount)
	}

	// at this point we should have different allocation and revision
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}
	if userStats.Allocated <= oldAllocation {
		t.Fatalf("The allocation count didn't update as expected for the authenticated user: %d", userStats.Allocated)
	}

	// effectively make a copy of the file by adding a test file under a different target path
	aliasedFilename := "testFolder/" + filename
	syncStatus, ulCount, err = cmdState.SyncFile(filename, aliasedFilename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != command.SyncStatusLocalNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if ulCount != 3 {
		t.Fatalf("The sync of the changed test file should have downloaded 3 chunks but it uploaded %d.", ulCount)
	}

	// at this point we should have different allocation and revision
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}
	if userStats.Allocated <= oldAllocation {
		t.Fatalf("The allocation count didn't update as expected for the authenticated user: %d", userStats.Allocated)
	}
	aliasedAllocation := userStats.Allocated - oldAllocation

	// confirm that there's a new file by getting the total list of files
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to get all of the file hashes: %v", err)
	} else if len(allFiles) != 3 {
		t.Fatalf("Expected to get a file hash listing of 3 files, but instead got one of length %d.", len(allFiles))
	}
	missingAliasedFile := true
	for _, fileData := range allFiles {
		decryptedRemoteFilename, err := cmdState.DecryptString(fileData.FileName)
		if err != nil {
			t.Fatalf("Failed to decrypt the remote filename: %v", err)
		}
		if strings.Compare(decryptedRemoteFilename, aliasedFilename) == 0 {
			missingAliasedFile = false
			break
		}
	}
	if missingAliasedFile {
		t.Fatalf("Aliased file (%s) didn't show up in the file hash list.", aliasedFilename)
	}

	// remove the aliased file and make sure the allocation count decreases by the same amount
	err = cmdState.RmFile(aliasedFilename)
	if err != nil {
		t.Fatalf("Failed to remove the aliased file from the server: %v", err)
	}
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}
	if userStats.Allocated != oldAllocation-aliasedAllocation {
		t.Fatalf("The allocation count didn't update as expected for the authenticated user: %d", userStats.Allocated)
	}

	// get a list of existing files for the user before testing the deletion of the user
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to get all of the file hashes for the test user: %v", err)
	}
	if len(allFiles) <= 0 {
		t.Fatalf("No files associated with the user to test with ...") // sanity test of the test
	}

	// remove the user
	err = cmdState.RmUser(state.Storage, username)
	if err != nil {
		t.Fatalf("Failed to remove the test user: %v", err)
	}

	// now use some raw API requests to see if we can get chunks, file infos, user stats
	for _, fileInfo := range allFiles {
		target := fmt.Sprintf("%s/api/chunk/%d/%d", cmdState.HostURI, fileInfo.FileID, fileInfo.CurrentVersion.VersionID)
		body, err = cmdState.RunAuthRequest(target, "GET", cmdState.AuthToken, nil)
		if len(body) > 0 {
			t.Fatalf("Chunk list obtained for file ID %d that should have been deleted.", fileInfo.FileID)
		}
		target = fmt.Sprintf("%s/api/file/%d", cmdState.HostURI, fileInfo.FileID)
		body, err = cmdState.RunAuthRequest(target, "GET", cmdState.AuthToken, nil)
		if len(body) > 0 {
			t.Fatalf("File information entry obtained for file ID %d that should have been deleted.", fileInfo.FileID)
		}
	}
	target = fmt.Sprintf("%s/api/user/stats", cmdState.HostURI)
	body, err = cmdState.RunAuthRequest(target, "GET", cmdState.AuthToken, nil)
	if len(body) > 0 {
		t.Fatalf("User stats obtained for the test user that should have been deleted.")
	}

	// add the user back into the server
	username = "admin"
	password = "1234"
	userQuota = int(1e9)
	user = cmdState.AddUser(state.Storage, username, password, userQuota)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	err = cmdState.Authenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}

	// wipe out the files that are in storage to start the syncdir operation with a clean state
	err = removeAllFilesFromStorage(cmdState)
	if err != nil {
		t.Fatalf("Couldn't remove all of the files from storage: %v", err)
	}

	// run a sync across the whole testdir directory
	syncdirCount, err := cmdState.SyncDirectory(testDataDir, testDataDir)
	if err != nil {
		t.Fatalf("Failed to run the syncdir command for the testdata directory: %v", err)
	}
	if syncdirCount != 10 {
		t.Fatalf("Expected to upload 10 chunks worth of data, but only uploaded %d.", syncdirCount)
	}

	// wipe out the files that are in storage to start the syncdir operation with a clean state
	err = removeAllFilesFromStorage(cmdState)
	if err != nil {
		t.Fatalf("Couldn't remove all of the files from storage: %v", err)
	}

	// run a sync across the whole testdir directory and specify a diffferent root remote folder
	syncdirCount, err = cmdState.SyncDirectory(testDataDir, "/master/"+testDataDir)
	if err != nil {
		t.Fatalf("Failed to run the syncdir command for the testdata directory: %v", err)
	}
	if syncdirCount != 10 {
		t.Fatalf("Expected to upload 10 chunks worth of data, but only uploaded %d.", syncdirCount)
	}

	// remove a local copy of a file
	err = os.Remove(testFilename1)
	if err != nil {
		t.Fatalf("Failed to remove the test file before attempting to sync: %v", err)
	}

	// run a sync again to download the file.
	syncdirCount, err = cmdState.SyncDirectory(testDataDir, "/master/"+testDataDir)
	if err != nil {
		t.Fatalf("Failed to run the syncdir command for the testdata directory: %v", err)
	}
	if syncdirCount != 3 {
		t.Fatalf("Expected to upload 3 chunks worth of data, but only uploaded %d.", syncdirCount)
	}

	// create an empty test file to make sure empty files
	os.Remove(testFilename4)
	emptyFile, err := os.Create(testFilename4)
	if err != nil {
		t.Fatalf("Failed to create an empty file %s: %v", testFilename4, err)
	}
	emptyFile.Close()

	// test to make sure we can sync the empty file
	syncStatus, ulCount, err = cmdState.SyncFile(testFilename4, testFilename4, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the empty file %s to the server: %v", testFilename4, err)
	}
	if syncStatus != command.SyncStatusLocalNewer {
		t.Fatalf("Empty file sync should be newer for file %s (%d)", testFilename4, syncStatus)
	}
	if ulCount != 0 {
		t.Fatalf("The sync of the empty test file should have uplodated zero chunks but it uploaded %d.", ulCount)
	}

	// remove the file
	os.Remove(testFilename4)

	// test downloading it through a sync
	syncStatus, dlCount, err = cmdState.SyncFile(testFilename4, testFilename4, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the empty file %s from the server: %v", testFilename4, err)
	}
	if syncStatus != command.SyncStatusRemoteNewer {
		t.Fatalf("Empty file sync should be not newer for file %s (%d)", testFilename4, syncStatus)
	}
	if ulCount != 0 {
		t.Fatalf("The sync of the empty test file should have downloaded zero chunks but it downloaded %d.", dlCount)
	}

	// create an empty directory and attempt to sync it
	err = os.MkdirAll(testDataDir3, os.ModeDir|os.FileMode(0777))
	if err != nil {
		t.Fatalf("Failed to create the empty test directory: %v", err)
	}
	syncStatus, dlCount, err = cmdState.SyncFile(testDataDir3, testDataDir3, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the empty directory %s to the server: %v", testDataDir3, err)
	}
	if syncStatus != command.SyncStatusLocalNewer {
		t.Fatalf("Empty dir sync should be not newer for dir %s (%d)", testFilename4, syncStatus)
	}
	if ulCount != 0 {
		t.Fatalf("The sync of the empty dir file should have downloaded zero chunks but it downloaded %d.", dlCount)
	}

	// syncing again should return the Same status
	syncStatus, dlCount, err = cmdState.SyncFile(testDataDir3, testDataDir3, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the empty directory %s from the server: %v", testDataDir3, err)
	}
	if syncStatus != command.SyncStatusSame {
		t.Fatalf("Empty dir should be the same on a repeat sync for dir %s (%d)", testFilename4, syncStatus)
	}
	if ulCount != 0 {
		t.Fatalf("The sync of the empty test dir should have downloaded zero chunks but it downloaded %d.", dlCount)
	}

	// deleting the directory and syncing again should pull it back from the server
	err = os.Remove(testDataDir3)
	if err != nil {
		t.Fatalf("Failed to delete the empty directory %s: %v", testDataDir3, err)
	}

	// syncing again should recreate the directory
	syncStatus, dlCount, err = cmdState.SyncFile(testDataDir3, testDataDir3, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to sync the empty directory %s from the server: %v", testDataDir3, err)
	}
	if syncStatus != command.SyncStatusRemoteNewer {
		t.Fatalf("Empty dir should be newer on a repeat sync for dir %s after deleting the directory (%d)", testFilename4, syncStatus)
	}
	if ulCount != 0 {
		t.Fatalf("The sync of the empty test dir should have downloaded zero chunks but it downloaded %d.", dlCount)
	}

	if _, err := os.Stat(testDataDir3); os.IsNotExist(err) {
		t.Fatalf("Empty directory should have been synced from the server but it was not created on the filesystem.")
	}
}

func TestFileVersioning(t *testing.T) {
	bytesAllocated := 0

	cmdState := command.NewState()

	// recreate a test user
	username := "admin"
	password := "1234"
	userQuota := int(1e9)

	user, err := state.Storage.GetUser("admin")
	if user != nil {
		cmdState.RmUser(state.Storage, username)
	}
	user = cmdState.AddUser(state.Storage, username, password, userQuota)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	err = cmdState.Authenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}

	err = cmdState.SetCryptoHashForPassword(*flagCryptoPass)
	if err != nil {
		t.Fatalf("Failed to set the crypto password for the test user: %v", err)
	}
	cmdState.CryptoKey, err = filefreezer.VerifyCryptoPassword(*flagCryptoPass, string(cmdState.CryptoHash))
	if err != nil {
		t.Fatalf("Failed to set the crypto key for the test user: %v", err)
	}

	// make sure to remove any files from storage
	err = removeAllFilesFromStorage(cmdState)
	if err != nil {
		t.Fatalf("Unable to remove all files from storage for the test user: %v", err)
	}

	// pull all the file infos ... should be empty
	allFiles, err := cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 0 {
		t.Fatalf("Expected to get an empty slice of FileInfo, but instead got one of length %d.", len(allFiles))
	}

	// regenerate some test file data
	rando1 := genRandomBytes(int(*flagServeChunkSize) * 3)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)

	///////////////////////////////////////////////////////////////////////////
	// upload initial version

	// add the file information to the storage server
	filename := testFilename1
	_, _, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Failed to at the file %s: %v", filename, err)
	}

	// verify we have the file registered
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 1 {
		t.Fatalf("Expected to a slice of one FileInfo object, but instead got one of length %d.", len(allFiles))
	}

	// make sure there's only one file version regiestered for the file
	versions, err := cmdState.GetFileVersions(filename)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("Expected to get one file version for the test file but received %d.", len(versions))
	}
	if versions[0].VersionNumber != 1 {
		t.Fatalf("The first version number for the test file was not 1, it was %d.", versions[0].VersionNumber)
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1) + 28*3 // bonus crypto for each chunk
	userStats, err := cmdState.GetUserStats()
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

	// upload a newer version of the file
	status, _, err := cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Error while updating file to a newer version via sync: %v", err)
	}
	if status != command.SyncStatusLocalNewer {
		t.Fatalf("Failed to correctly sync the second version of a test file: status wasn't local newer (%v).", status)
	}

	// verify we have only the one file registered
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 1 {
		t.Fatalf("Expected to a slice of one FileInfo object, but instead got one of length %d.", len(allFiles))
	}

	// verify that we get two versions back for the given file ID
	versions, err = cmdState.GetFileVersions(filename)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("Expected to get two file versions for the test file but received %d.", len(versions))
	}

	// set this variable to use later on to test reverting back to a previous
	// version of a file
	callbackVersion := versions[1]
	callbackBytes := rando1

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1) + 28*3 // bonus crypto for each chunk
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// modify all existing chunks and upload a new version
	rando1 = genRandomBytes(int(*flagServeChunkSize) * 3)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)

	// upload a newer version of the file
	status, _, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Error while updating file to a newer version via sync: %v", err)
	}
	if status != command.SyncStatusLocalNewer {
		t.Fatalf("Failed to correctly sync the third version of a test file: status wasn't local newer (%v).", status)
	}

	// verify we have only the one file registered
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 1 {
		t.Fatalf("Expected to a slice of one FileInfo object, but instead got one of length %d.", len(allFiles))
	}

	// verify that we get three versions back for the given file ID
	versions, err = cmdState.GetFileVersions(filename)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("Expected to get three file versions for the test file but received %d.", len(versions))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1) + 28*3 // bonus crypto for each chunk
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// make a larger file and upload a new version
	rando1 = genRandomBytes(int(*flagServeChunkSize) * 6)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)

	// upload a newer version of the file
	status, _, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Error while updating file to a newer version via sync: %v", err)
	}
	if status != command.SyncStatusLocalNewer {
		t.Fatalf("Failed to correctly sync the fourth version of a test file: status wasn't local newer (%v).", status)
	}

	// verify we have only the one file registered
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 1 {
		t.Fatalf("Expected to a slice of one FileInfo object, but instead got one of length %d.", len(allFiles))
	}

	// verify that we get four versions back for the given file ID
	versions, err = cmdState.GetFileVersions(filename)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versions) != 4 {
		t.Fatalf("Expected to get four versions for the test file but received %d.", len(versions))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1) + 28*6 // bonus crypto for each chunk
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// make the file smaller and upload a new version
	rando1 = rando1[:(int(*flagServeChunkSize)*2)-1]
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)

	// upload a newer version of the file
	status, _, err = cmdState.SyncFile(filename, filename, command.SyncCurrentVersion)
	if err != nil {
		t.Fatalf("Error while updating file to a newer version via sync: %v", err)
	}
	if status != command.SyncStatusLocalNewer {
		t.Fatalf("Failed to correctly sync the fifth version of a test file: status wasn't local newer (%v).", status)
	}

	// verify we have only the one file registered
	allFiles, err = cmdState.GetAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 1 {
		t.Fatalf("Expected to a slice of one FileInfo object, but instead got one of length %d.", len(allFiles))
	}

	// verify that we get five versions back for the given file ID
	versions, err = cmdState.GetFileVersions(filename)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versions) != 5 {
		t.Fatalf("Expected to get five file versions for the test file but received %d.", len(versions))
	}

	// make sure the user quota updated correctly
	bytesAllocated += len(rando1) + 28*2 // bonus crypto for each chunk
	userStats, err = cmdState.GetUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats for the test user: %v", err)
	}
	if userStats.Allocated != bytesAllocated {
		t.Fatalf("Expected %d bytes allocated but the server returned %d.", bytesAllocated, userStats.Allocated)
	}

	///////////////////////////////////////////////////////////////////////////
	// get a previous version of the file
	status, _, err = cmdState.SyncFile(filename, filename, callbackVersion.VersionNumber)
	if err != nil {
		t.Fatalf("Error while updating file to a pervious version via sync: %v", err)
	}
	if status != command.SyncStatusRemoteNewer {
		t.Fatalf("Failed to correctly sync the previous version of a test file: status wasn't remote newer (%v).", status)
	}

	// verify that the file got reverted back to the previous version bytes
	previousBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("Error while attempting to read local file to verify sync of a previous version: %v", err)
	}

	if bytes.Compare(previousBytes, callbackBytes) != 0 {
		t.Fatal("Differences were found in the local file with respect to previous version when they should have been the same")
	}

	// at this point we have five versions. attempt to delete the first three
	err = cmdState.RmFileVersions(filename, 1, 3)
	if err != nil {
		t.Fatalf("Error while attempting to remove the first three versions of the test file: %v", err)
	}

	// verify that we only have two versions left
	versions, err = cmdState.GetFileVersions(filename)
	if err != nil {
		t.Fatalf("Failed to get the file versions for the test file: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("Expected to get two file versions for the test file but received %d.", len(versions))
	}
	if versions[0].VersionNumber != 4 || versions[1].VersionNumber != 5 {
		t.Fatalf("Expected to get file versions 4 and 5 for the test file but got %d and %d instead.",
			versions[0].VersionNumber, versions[1].VersionNumber)
	}
}

func removeAllFilesFromStorage(cmdState *command.State) error {
	// get all of the remote file names
	allRemoteFiles, err := cmdState.GetAllFileHashes()
	if err != nil {
		return fmt.Errorf("Failed to get all of the remote files to remove: %v", err)
	}

	// for each remote file, execute a remove function
	for _, fileHash := range allRemoteFiles {
		err = cmdState.RmFileByID(fileHash.FileID)
		if err != nil {
			return fmt.Errorf("Failed to remove the remote file id %d: %v", fileHash.FileID, err)
		}
	}

	return nil
}

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
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

const (
	useHTTPS       = false
	testServerAddr = ":8080"
	testDataDir    = "testdata"
	testDataDir2   = "testdata/subdir"
	testFilename1  = "testdata/unit_test_1.dat"
	testFilename2  = "testdata/unit_test_2.dat"
	testFilename3  = "testdata/subdir/unit_test_3.dat"
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
	*flagPublicKeyPath = "freezer.rsa.pub"
	*flagPrivateKeyPath = "freezer.rsa"
	if useHTTPS {
		setupHTTPSTestFlags()
	} else {
		setupHTTPTestFlags()
	}

	// make sure the test data folder exists
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
	cmdState := newCommandState()

	// create a test user
	username := "admin"
	password := "1234"
	userQuota := int(1e9)
	user := cmdState.addUser(state.Storage, username, password, userQuota)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	err := cmdState.authenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}
	if cmdState.serverCapabilities.ChunkSize != *flagServeChunkSize {
		t.Fatalf("Server capabilities returned a different chunk size than configured for the test: %d", *flagServeChunkSize)
	}

	// getting the user stats now should have default quota and otherwise empty settings
	userStats, err := cmdState.getUserStats()
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
	allFiles, err := cmdState.getAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 0 {
		t.Fatalf("Expected to get an empty slice of FileInfo, but instead got one of length %d.", len(allFiles))
	}
	t.Logf("Got all of the file names (%d) ...", len(allFiles))

	// the revision should not have changed by only getting the file hashes
	oldRevision := userStats.Revision
	userStats, err = cmdState.getUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// test adding a file
	filename := testFilename1
	chunkCount, lastMod, permissions, hashString, err := filefreezer.CalcFileHashInfo(cmdState.serverCapabilities.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", filename, err)
	}
	t.Logf("Calculated hash data for %s ...", filename)

	fileID, err := cmdState.addFile(filename, filename, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to at the file %s: %v", filename, err)
	}
	t.Logf("Added file %s (id: %d) ...", filename, fileID)

	// at this point we should have a different revision
	oldRevision = userStats.Revision
	userStats, err = cmdState.getUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// now that the file is registered, sync the data
	syncStatus, ulCount, err := cmdState.syncFile(filename, filename)
	if err != nil {
		t.Fatalf("Failed to sync the file %s to the server: %v", filename, err)
	}
	if syncStatus != syncStatusSame {
		t.Fatalf("Initial sync after add should be identical for file %s", filename)
	}
	if ulCount != 0 {
		t.Fatalf("The first sync of the first test file should be identical, but sync said %d chunks were uploaded.", ulCount)
	}
	t.Logf("Synced the file %s ...", filename)

	// at this point we should have the same revision because the file was unchanged
	oldRevision = userStats.Revision
	userStats, err = cmdState.getUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// now we get a chunk list for the file
	var remoteChunks models.FileChunksGetResponse
	target := fmt.Sprintf("%s/api/chunk/%d/%d", cmdState.hostURI, fileID, DEBUG_VERSION_MAGIC)
	body, err := runAuthRequest(target, "GET", cmdState.authToken, nil)
	err = json.Unmarshal(body, &remoteChunks)
	if err != nil {
		t.Fatalf("Failed to get the file chunk list for the file name given (%s): %v", filename, err)
	}
	if len(remoteChunks.Chunks) != chunkCount {
		t.Fatalf("The synced file %s doesn't have the correct number of chunks on the server (got:%d expected:%d). %v",
			filename, len(remoteChunks.Chunks), chunkCount, remoteChunks)
	}

	// at this point we should have the same revision
	oldRevision = userStats.Revision
	userStats, err = cmdState.getUserStats()
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
	syncStatus, ulCount, err = cmdState.syncFile(filename, filename)
	if err != nil {
		t.Fatalf("Failed to sync the file %s to the server: %v", filename, err)
	}
	if syncStatus != syncStatusLocalNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if ulCount != 3 {
		t.Fatalf("The first sync of the changed test file should have uploaded 3 chunks but it uploaded %d.", ulCount)
	}

	// set the old revision count here to test below and make sure that
	// allocation counts stayed the same since the file synced above is the same size
	oldAllocation := userStats.Allocated
	userStats, err = cmdState.getUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Allocated != oldAllocation {
		t.Fatalf("Allocation counts changed for syncing a file of the same size.")
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
	syncStatus, dlCount, err := cmdState.syncFile(filename, filename)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != syncStatusRemoteNewer {
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
	userStats, err = cmdState.getUserStats()
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
	syncStatus, dlCount, err = cmdState.syncFile(filename, filename)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != syncStatusRemoteNewer {
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
	userStats, err = cmdState.getUserStats()
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// test syncing a file not registered on the server
	filename = testFilename2
	syncStatus, ulCount, err = cmdState.syncFile(filename, filename)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != syncStatusLocalNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if ulCount != 3 {
		t.Fatalf("The sync of the changed test file should have downloaded 3 chunks but it downloaded %d.", dlCount)
	}

	// at this point we should have different allocation and revision
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = cmdState.getUserStats()
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
	syncStatus, ulCount, err = cmdState.syncFile(filename, aliasedFilename)
	if err != nil {
		t.Fatalf("Failed to sync the file %s from the server: %v", filename, err)
	}
	if syncStatus != syncStatusLocalNewer {
		t.Fatalf("Sync after regeneration should be newer for file %s (%d)", filename, syncStatus)
	}
	if ulCount != 3 {
		t.Fatalf("The sync of the changed test file should have downloaded 3 chunks but it downloaded %d.", dlCount)
	}

	// at this point we should have different allocation and revision
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = cmdState.getUserStats()
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
	allFiles, err = cmdState.getAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to get all of the file hashes: %v", err)
	} else if len(allFiles) != 3 {
		t.Fatalf("Expected to get a file hash listing of 3 files, but instead got one of length %d.", len(allFiles))
	}
	missingAliasedFile := true
	for _, fileData := range allFiles {
		if strings.Compare(fileData.FileName, aliasedFilename) == 0 {
			missingAliasedFile = false
			break
		}
	}
	if missingAliasedFile {
		t.Fatalf("Alised file (%s) didn't show up in the file hash list.", aliasedFilename)
	}

	// remove the aliased file and make sure the allocation count decreases by the same amount
	err = cmdState.rmFile(aliasedFilename)
	if err != nil {
		t.Fatalf("Failed to remove the aliased file from the server: %v", err)
	}
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = cmdState.getUserStats()
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
	allFiles, err = cmdState.getAllFileHashes()
	if err != nil {
		t.Fatalf("Failed to get all of the file hashes for the test user: %v", err)
	}
	if len(allFiles) <= 0 {
		t.Fatalf("No files associated with the user to test with ...") // sanity test of the test
	}

	// remove the user
	err = cmdState.rmUser(state.Storage, username)
	if err != nil {
		t.Fatalf("Failed to remove the test user: %v", err)
	}

	// now use some raw API requests to see if we can get chunks, file infos, user stats
	for _, fileInfo := range allFiles {
		target := fmt.Sprintf("%s/api/chunk/%d/%d", cmdState.hostURI, fileInfo.FileID, DEBUG_VERSION_MAGIC)
		body, err = runAuthRequest(target, "GET", cmdState.authToken, nil)
		if len(body) > 0 {
			t.Fatalf("Chunk list obtained for file ID %d that should have been deleted.", fileInfo.FileID)
		}
		target = fmt.Sprintf("%s/api/file/%d", cmdState.hostURI, fileInfo.FileID)
		body, err = runAuthRequest(target, "GET", cmdState.authToken, nil)
		if len(body) > 0 {
			t.Fatalf("File information entry obtained for file ID %d that should have been deleted.", fileInfo.FileID)
		}
	}
	target = fmt.Sprintf("%s/api/user/stats", cmdState.hostURI)
	body, err = runAuthRequest(target, "GET", cmdState.authToken, nil)
	if len(body) > 0 {
		t.Fatalf("User stats obtained for the test user that should have been deleted.")
	}

	// add the user back into the server
	username = "admin"
	password = "1234"
	userQuota = int(1e9)
	user = cmdState.addUser(state.Storage, username, password, userQuota)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	err = cmdState.authenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}

	// wipe out the files that are in storage to start the syncdir operation with a clean state
	err = removeAllFilesFromStorage(cmdState)
	if err != nil {
		t.Fatalf("Couldn't remove all of the files from storage: %v", err)
	}

	// run a sync across the whole testdir directory
	syncdirCount, err := cmdState.syncDirectory(testDataDir, testDataDir)
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
	syncdirCount, err = cmdState.syncDirectory(testDataDir, "/master/"+testDataDir)
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
	syncdirCount, err = cmdState.syncDirectory(testDataDir, "/master/"+testDataDir)
	if err != nil {
		t.Fatalf("Failed to run the syncdir command for the testdata directory: %v", err)
	}
	if syncdirCount != 3 {
		t.Fatalf("Expected to upload 3 chunks worth of data, but only uploaded %d.", syncdirCount)
	}

}

func removeAllFilesFromStorage(cmdState *commandState) error {
	// get all of the remote file names
	allRemoteFiles, err := cmdState.getAllFileHashes()
	if err != nil {
		return fmt.Errorf("Failed to get all of the remote files to remove: %v", err)
	}

	// for each remote file, execute a remove function
	for _, fileHash := range allRemoteFiles {
		err = cmdState.rmFile(fileHash.FileName)
		if err != nil {
			return fmt.Errorf("Failed to remove the remote files %s: %v", fileHash.FileName, err)
		}
	}

	return nil
}

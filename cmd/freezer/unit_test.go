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
	testServerAddr = ":8080"
	testHost       = "https://127.0.0.1:8080"
	testDataDir    = "testdata"
	testFilename1  = "testdata/unit_test_1.dat"
	testFilename2  = "testdata/unit_test_2.dat"
)

var (
	state *models.State
	AppFs afero.Fs = afero.NewOsFs()
)

// TODO: make a test upload a file exactly 2xChunkSize and then sync it

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

func TestMain(m *testing.M) {
	// instead of using command line flags for the unit test, we'll just
	// override the flag values right here
	*flagDatabasePath = "file::memory:?mode=memory&cache=shared"
	//*flagDatabasePath = "file:unit_test.db"
	*flagPublicKeyPath = "freezer.rsa.pub"
	*flagPrivateKeyPath = "freezer.rsa"
	*flagTLSKey = "freezer.key"
	*flagTLSCrt = "freezer.crt"
	*flagChunkSize = 1024 * 1024 * 4
	*flagExtraStrict = true
	*argListenAddr = testServerAddr

	// make sure the test data folder exists
	os.Mkdir(testDataDir, os.ModeDir|os.ModePerm)

	// write out some random files
	rand.Seed(time.Now().Unix())
	rando1 := genRandomBytes(int(*flagChunkSize) * 3)
	ioutil.WriteFile(testFilename1, rando1, os.ModePerm)
	rando2 := genRandomBytes(int(*flagChunkSize)*2 + 42)
	ioutil.WriteFile(testFilename2, rando2, os.ModePerm)

	// run a new state in a server
	var err error
	state, err = newState()
	if err != nil {
		log.Fatalf("Unable to initialize the server: %v", err)
	}
	defer closeState(state)

	// this new server will run in a separate goroutine
	readyCh := make(chan bool)
	go runServe(state, readyCh)

	<-readyCh
	os.Exit(m.Run())
}

func TestEverything(t *testing.T) {
	// create a test user
	username := "admin"
	password := "1234"
	userQuota := int(1e9)
	user := runAddUser(state.Storage, username, password, userQuota)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	token, err := runUserAuthenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}
	t.Logf("User authenticated ...")

	// getting the user stats now should have default quota and otherwise empty settings
	userStats, err := runUserStats(testHost, token)
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
	allFiles, err := runGetAllFileHashes(testHost, token)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 0 {
		t.Fatalf("Expected to get an empty slice of FileInfo, but instead got one of length %d.", len(allFiles))
	}
	t.Logf("Got all of the file names (%d) ...", len(allFiles))

	// the revision should not have changed by only getting the file hashes
	oldRevision := userStats.Revision
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// test adding a file
	filename := testFilename1
	chunkCount, lastMod, hashString, err := filefreezer.CalcFileHashInfo(state.Storage.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", filename, err)
	}
	t.Logf("Calculated hash data for %s ...", filename)

	fileID, err := runAddFile(testHost, token, filename, filename, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to at the file %s: %v", filename, err)
	}
	t.Logf("Added file %s (id: %d) ...", filename, fileID)

	// at this point we should have a different revision
	oldRevision = userStats.Revision
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// now that the file is registered, sync the data
	syncStatus, ulCount, err := runSyncFile(testHost, token, filename, filename)
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
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// now we get a chunk list for the file
	var remoteChunks models.FileChunksGetResponse
	target := fmt.Sprintf("%s/api/chunk/%d", testHost, fileID)
	body, err := runAuthRequest(target, "GET", token, nil)
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
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// sleep a second then regenerate the file
	time.Sleep(time.Second)
	rando1 := genRandomBytes(int(*flagChunkSize * 3))
	ioutil.WriteFile(filename, rando1, os.ModePerm)

	// now that the file is regenerated, sync the data
	syncStatus, ulCount, err = runSyncFile(testHost, token, filename, filename)
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
	userStats, err = runUserStats(testHost, token)
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
	syncStatus, dlCount, err := runSyncFile(testHost, token, filename, filename)
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
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// generate some new test bytes
	frankenBytes := genRandomBytes(int(*flagChunkSize) * 3)
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
	syncStatus, dlCount, err = runSyncFile(testHost, token, filename, filename)
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
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision != oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}

	// test syncing a file not registered on the server
	filename = testFilename2
	syncStatus, ulCount, err = runSyncFile(testHost, token, filename, filename)
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
	userStats, err = runUserStats(testHost, token)
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
	syncStatus, ulCount, err = runSyncFile(testHost, token, filename, aliasedFilename)
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
	userStats, err = runUserStats(testHost, token)
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
	allFiles, err = runGetAllFileHashes(testHost, token)
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
	err = runRmFile(testHost, token, aliasedFilename)
	if err != nil {
		t.Fatalf("Failed to remove the aliased file from the server: %v", err)
	}
	oldAllocation = userStats.Allocated
	oldRevision = userStats.Revision
	userStats, err = runUserStats(testHost, token)
	if err != nil {
		t.Fatalf("Failed to get the user stats: %v", err)
	}
	if userStats.Revision <= oldRevision {
		t.Fatalf("The revision count didn't update as expected for the authenticated user: %d", userStats.Revision)
	}
	if userStats.Allocated != oldAllocation-aliasedAllocation {
		t.Fatalf("The allocation count didn't update as expected for the authenticated user: %d", userStats.Allocated)
	}

}

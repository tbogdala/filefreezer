// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"log"
	"os"
	"testing"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

const (
	testServerAddr = ":8080"
	testHost       = "http://127.0.0.1:8080"
)

var (
	state *models.State
)

func TestMain(m *testing.M) {
	// instead of using command line flags for the unit test, we'll just
	// override the flag values right here
	*flagDatabasePath = "file::memory:?mode=memory&cache=shared"
	*flagPublicKeyPath = "freezer.rsa.pub"
	*flagPrivateKeyPath = "freezer.rsa"
	*flagChunkSize = 1024 * 1024 * 4
	*argListenAddr = testServerAddr

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
	user := runAddUser(state.Storage, username, password, 1e9)
	if user == nil {
		t.Fatalf("Failed to add the test user (%s) to Storage", username)
	}

	// attempt to get the authentication token
	token, err := runUserAuthenticate(testHost, username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	}
	t.Logf("User authenticated ...")

	// pull all the file infos ... should be empty
	allFiles, err := runGetAllFileHashes(testHost, token)
	if err != nil {
		t.Fatalf("Failed to authenticate as the test user: %v", err)
	} else if len(allFiles) != 0 {
		t.Fatalf("Expected to get an empty slice of FileInfo, but instead got one of length %d.", len(allFiles))
	}
	t.Logf("Got all of the file names (%d) ...", len(allFiles))

	// test adding a file
	filename := "main.go"
	chunkCount, lastMod, hashString, err := filefreezer.CalcFileHashInfo(state.Storage.ChunkSize, filename)
	if err != nil {
		t.Fatalf("Failed to calculate the file hash for %s: %v", filename, err)
	}
	t.Logf("Calculated hash data for %s ...", filename)

	fileID, err := runAddFile(testHost, token, filename, lastMod, chunkCount, hashString)
	if err != nil {
		t.Fatalf("Failed to at the file %s: %v", filename, err)
	}
	t.Logf("Added file %s (id: %d) ...", filename, fileID)
}

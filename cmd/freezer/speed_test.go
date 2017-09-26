// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/command"
)

func setupBenchmarkState(b *testing.B) *command.State {
	cryptoPass := "beavers_and_ducks"

	// create a test user
	cmdState := command.NewState()
	cmdState.SetQuiet(true)

	username := "adminBench"
	password := "1234"
	userQuota := int(1e9)

	// attempt to get the authentication token set in the command state
	err := cmdState.Authenticate(testHost, username, password)
	if err != nil {
		cmdState.AddUser(state.Storage, username, password, userQuota)
		err := cmdState.Authenticate(testHost, username, password)
		if err != nil {
			b.Fatalf("Failed to authenticate as the test user: %v", err)
		}
	}

	if len(cmdState.CryptoHash) == 0 {
		err = cmdState.SetCryptoHashForPassword(cryptoPass)
		if err != nil {
			b.Fatalf("Failed to set the crypto password for the test user: %v", err)
		}
	}
	cmdState.CryptoKey, err = filefreezer.VerifyCryptoPassword(cryptoPass, string(cmdState.CryptoHash))
	if err != nil {
		b.Fatalf("Failed to set the crypto key for the test user: %v", err)
	}

	return cmdState
}

func BenchmarkBasicFileSyncUp4KB(b *testing.B) {
	doBenchBasicFileSyncUp(1024*4, b)
}

func BenchmarkBasicFileSyncUp4MB(b *testing.B) {
	doBenchBasicFileSyncUp(1024*1024*4, b)
}

func doBenchBasicFileSyncUp(testFileSize int, b *testing.B) {
	// create the server and command states
	cmdState := setupBenchmarkState(b)

	// write the test file to the filesystem
	testFilename := "bench_data.dat"
	randoBytes := genRandomBytes(testFileSize)
	ioutil.WriteFile(testFilename, randoBytes, os.ModePerm)

	b.ResetTimer()

	// loop: sync a file
	for n := 0; n < b.N; n++ {
		destFilename := fmt.Sprintf("bench_data_%08d.dat", n)
		_, _, err := cmdState.SyncFile(testFilename, destFilename, command.SyncCurrentVersion)
		if err != nil {
			b.Fatalf("Failed to at the file %s: %v", testFilename, err)
		}
	}
}

func BenchmarkBasicFileSyncDown4KB(b *testing.B) {
	doBenchBasicFileSyncDown(1024*4, b)
}

func BenchmarkBasicFileSyncDown4MB(b *testing.B) {
	doBenchBasicFileSyncDown(1024*1024*4, b)
}

func doBenchBasicFileSyncDown(testFileSize int, b *testing.B) {
	// create the server and command states
	cmdState := setupBenchmarkState(b)

	// write the test file to the filesystem
	testFilename := "bench_data.dat"
	randoBytes := genRandomBytes(testFileSize)
	ioutil.WriteFile(testFilename, randoBytes, os.ModePerm)

	// test adding a file
	fileStats, err := filefreezer.CalcFileHashInfo(cmdState.ServerCapabilities.ChunkSize, testFilename)
	if err != nil {
		b.Fatalf("Failed to calculate the file hash for %s: %v", testFilename, err)
	}

	// sync the test file to the server
	_, _, err = cmdState.SyncFile(testFilename, testFilename, command.SyncCurrentVersion)
	//_, err = cmdState.addFile(testFilename, testFilename, false, permissions, lastMod, chunkCount, hashString)
	if err != nil {
		b.Fatalf("Failed to at the file %s: %v", testFilename, err)
	}

	// remove the original copy
	err = os.Remove(testFilename)
	if err != nil {
		b.Fatalf("Couldn't remove file just synced from server: %v", err)
	}

	b.ResetTimer()

	// loop: sync a file
	for n := 0; n < b.N; n++ {
		localFilename := fmt.Sprintf("bench_data_local_%08d.dat", n)
		status, changeCount, err := cmdState.SyncFile(localFilename, testFilename, command.SyncCurrentVersion)
		if err != nil {
			b.Fatalf("Failed to sync the file %s from the server: %v", localFilename, err)
		}
		if status != command.SyncStatusRemoteNewer {
			b.Fatal("Benchmark sync should find the remote file newer.")
		}
		if changeCount != fileStats.ChunkCount {
			b.Fatalf("The sync of the test file should be identical to the source, but sync said %d chunks were uploaded.", fileStats.ChunkCount)
		}

		// remove the local copy of the file
		b.StopTimer()
		err = os.Remove(localFilename)
		if err != nil {
			b.Fatalf("Couldn't remove file just synced from server: %v", err)
		}
		b.StartTimer()
	}
}

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package tests

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/tbogdala/filefreezer"
)

func setupBenchmarkStorage(dbPath string, b *testing.B) (*filefreezer.Storage, *filefreezer.User) {
	// create an in memory storage
	store, err := filefreezer.NewStorage(dbPath)
	if err != nil {
		b.Fatalf("Failed to create the in-memory storage for testing. %v", err)
	}
	store.CreateTables()

	// attempt to add a user
	username := "admin"
	password := "1234"
	salt, saltedPass, err := filefreezer.GenLoginPasswordHash(password)
	if err != nil {
		b.Fatalf("Failed to generate a password hash %v", err)
	}
	user, err := store.AddUser(username, salt, saltedPass, 1e12)
	if err != nil || user == nil {
		b.Fatalf("Failed to add a new user (%s) to storage: %v", username, err)
	}

	return store, user
}

func setupRandomBytes(size int) (randoBytes []byte, hashString string) {
	randoBytes = genRandomBytes(size)
	hasher := sha1.New()
	hasher.Write(randoBytes)
	hash := hasher.Sum(nil)
	hashString = base64.URLEncoding.EncodeToString(hash)

	return randoBytes, hashString
}

func BenchmarkBasicFileWriteMemory4KB(b *testing.B) {
	doBenchFileWrite("file::memory:?mode=memory&cache=shared", 1024*4, b)
}

func BenchmarkBasicFileWriteFilesystem4KB(b *testing.B) {
	const filename = "write_bench1.db"
	os.Remove(filename)
	doBenchFileWrite(filename, 1024*4, b)
	os.Remove(filename)
}

func BenchmarkBasicFileWriteMemory4MB(b *testing.B) {
	doBenchFileWrite("file::memory:?mode=memory&cache=shared", 1024*1024*4, b)
}

func BenchmarkBasicFileWriteFilesystem4MB(b *testing.B) {
	const filename = "write_bench2.db"
	os.Remove(filename)
	doBenchFileWrite(filename, 1024*1024*4, b)
	os.Remove(filename)
}

func doBenchFileWrite(dbPath string, chunkSize int, b *testing.B) {
	store, user := setupBenchmarkStorage(dbPath, b)
	defer store.Close()

	randoBytes, hashString := setupRandomBytes(chunkSize)
	modTime := time.Now().Unix()
	b.ResetTimer()

	// loop: create a file with one chunk and upload the chunk
	for n := 0; n < b.N; n++ {
		fi, err := store.AddFileInfo(user.ID, fmt.Sprintf("TestFile_%8d.dat", n), false, 0777, modTime, 1, hashString)
		if err != nil {
			b.Fatalf("Failed to add a test file for iteration %d: %v", n, err)
		}

		_, err = store.AddFileChunk(user.ID, fi.FileID, fi.CurrentVersion.VersionID, 0, hashString, randoBytes)
		if err != nil {
			b.Fatalf("Failed to add a test fchunkile for iteration %d: %v", n, err)
		}
	}
}

func BenchmarkBasicChunkReadMemory4KB(b *testing.B) {
	doBenchReadChunk("file::memory:?mode=memory&cache=shared", 1024*4, b)
}

func BenchmarkBasicChunkReadFilesystem4KB(b *testing.B) {
	const filename = "read_bench1.db"
	os.Remove(filename)
	doBenchReadChunk(filename, 1024*1024*4, b)
	os.Remove(filename)
}

func BenchmarkBasicChunkReadMemory4MB(b *testing.B) {
	doBenchReadChunk("file::memory:?mmode=memory&cache=shared", 1024*4, b)
}

func BenchmarkBasicChunkReadFilesystem4MB(b *testing.B) {
	const filename = "read_bench2.db"
	os.Remove(filename)
	doBenchReadChunk(filename, 1024*1024*4, b)
	os.Remove(filename)
}

func doBenchReadChunk(dbPath string, chunkSize int, b *testing.B) {
	store, user := setupBenchmarkStorage(dbPath, b)
	defer store.Close()

	randoBytes, hashString := setupRandomBytes(chunkSize)
	modTime := time.Now().Unix()

	// create a file with one chunk and upload the chunk
	fi, err := store.AddFileInfo(user.ID, "TestFile_00.dat", false, 0777, modTime, 1, hashString)
	if err != nil {
		b.Fatalf("Failed to add a test file: %v", err)
	}

	_, err = store.AddFileChunk(user.ID, fi.FileID, fi.CurrentVersion.VersionID, 0, hashString, randoBytes)
	if err != nil {
		b.Fatalf("Failed to add a test chunk: %v", err)
	}

	b.ResetTimer()

	// attempt reads of that chunk
	for n := 0; n < b.N; n++ {
		_, err = store.GetFileChunk(fi.FileID, 0, fi.CurrentVersion.VersionID)
		if err != nil {
			b.Fatalf("Failed to get a file chunk from storage for iteration %d: %v", n, err)
		}
	}
}

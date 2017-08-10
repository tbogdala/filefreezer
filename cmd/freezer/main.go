// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/tbogdala/filefreezer"

	"strings"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

// User kingpin to define a set of commands and flags for the application.
var (
	appFlags           = kingpin.New("freezer", "A web application server for FileFreezer.")
	flagDatabasePath   = appFlags.Flag("db", "The database path.").Default("file:freezer.db").String()
	flagPublicKeyPath  = appFlags.Flag("pub", "The file path to the public key.").Default("freezer.rsa.pub").String()
	flagPrivateKeyPath = appFlags.Flag("priv", "The file path to the private key.").Default("freezer.rsa").String()
	flagTLSKey         = appFlags.Flag("tlskey", "The HTTPS TLS private key file.").String()
	flagTLSCrt         = appFlags.Flag("tlscert", "The HTTPS TLS public crt file.").String()
	flagExtraStrict    = appFlags.Flag("xs", "File checking should be extra strict on file sync comparisons.").Default("true").Bool()
	flagUserName       = appFlags.Flag("user", "The username for user.").Short('u').String()
	flagUserPass       = appFlags.Flag("pass", "The password for user.").Short('p').String()

	cmdServe           = appFlags.Command("serve", "Adds a new user to the storage.")
	argServeListenAddr = cmdServe.Arg("http", "The net address to listen to").Default(":8080").String()
	flagServeChunkSize = cmdServe.Flag("cs", "The number of bytes contained in one chunk.").Default("4194304").Int64() // 4 MB

	cmdAddUser       = appFlags.Command("adduser", "Adds a new user to the storage.")
	flagAddUserQuota = cmdAddUser.Flag("newquota", "The quota size in bytes.").Short('q').Default("1000000000").Int()

	cmdRmUser = appFlags.Command("rmuser", "Removes a user from the storage.")

	cmdModUser          = appFlags.Command("moduser", "Modifies a user in storage.")
	flagModUserNewQuota = cmdModUser.Flag("newquota", "New quota size in bytes.").Short('Q').Int()
	flagModUserNewName  = cmdModUser.Flag("newuser", "New quota size in bytes.").Short('U').String()
	flagModUserNewPass  = cmdModUser.Flag("newpass", "New quota size in bytes.").Short('P').String()

	cmdUserStats     = appFlags.Command("userstats", "Gets the quota, allocation and revision counts for the user.")
	argUserStatsHost = cmdUserStats.Arg("hostname", "The host URI for the storage server to contact.").Required().String()

	cmdGetFiles     = appFlags.Command("getfiles", "Gets all files for a user in storage.")
	argGetFilesHost = cmdGetFiles.Arg("hostname", "The host URI for the storage server to contact.").Required().String()

	cmdAddFile       = appFlags.Command("addfile", "Put a file into storage.")
	argAddFileHost   = cmdAddFile.Arg("hostname", "The host URI for the storage server to contact.").Required().String()
	argAddFilePath   = cmdAddFile.Arg("filename", "The local file to put on the server.").Required().String()
	argAddFileTarget = cmdAddFile.Arg("target", "The file path to use on the server for the local file; defaults to the same as the filename arg.").Default("").String()

	cmdRmFile     = appFlags.Command("rmfile", "Remove a file from storage.")
	argRmFileHost = cmdRmFile.Arg("hostname", "The host URI for the storage server to contact.").Required().String()
	argRmFilePath = cmdRmFile.Arg("filename", "The file to remove on the server.").Required().String()

	cmdSync       = appFlags.Command("sync", "Synchronizes a path with the server.")
	argSyncHost   = cmdSync.Arg("hostname", "The host URI for the storage server to contact.").Required().String()
	argSyncPath   = cmdSync.Arg("filepath", "The file to sync with the server.").Required().String()
	argSyncTarget = cmdSync.Arg("target", "The file path to sync to on the server; defaults to the same as the filename arg.").Default("").String()

	cmdSyncDir       = appFlags.Command("syncdir", "Synchronizes a directory with the server.")
	argSyncDirHost   = cmdSyncDir.Arg("hostname", "The host URI for the storage server to contact.").Required().String()
	argSyncDirPath   = cmdSyncDir.Arg("dirpath", "The directory to sync with the server.").Required().String()
	argSyncDirTarget = cmdSyncDir.Arg("target", "The directory path to sync to on the server; defaults to the same as the filename arg.").Default("").String()
)

// openStorage is the common function used to open the filefreezer Storage
func openStorage() (*filefreezer.Storage, error) {
	log.Printf("Opening database: %s\n", *flagDatabasePath)

	// open up the storage database
	store, err := filefreezer.NewStorage(*flagDatabasePath)
	if err != nil {
		return nil, err
	}
	store.CreateTables()
	return store, nil
}

func interactiveGetUser() string {
	if *flagUserName != "" {
		return *flagUserName
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Username: ")
	username, _ := reader.ReadString('\n')
	return strings.TrimSpace(username)
}

func interactiveGetPassword() string {
	if *flagUserPass != "" {
		return *flagUserPass
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Password: ")
	password, _ := reader.ReadString('\n')
	return strings.TrimSpace(password)
}

func main() {
	switch kingpin.MustParse(appFlags.Parse(os.Args[1:])) {
	case cmdServe.FullCommand():
		// setup a new server state or exit out on failure
		state, err := newState()
		if err != nil {
			log.Fatalf("Unable to initialize the server: %v", err)
		}
		defer state.close()
		state.Storage.ChunkSize = *flagServeChunkSize
		state.serve(nil)

	case cmdAddUser.FullCommand():
		store, err := openStorage()
		if err != nil {
			log.Fatalf("Failed to open the storage database: %v", err)
		}
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()
		cmdState.addUser(store, username, password, *flagAddUserQuota)

	case cmdRmUser.FullCommand():
		store, err := openStorage()
		if err != nil {
			log.Fatalf("Failed to open the storage database: %v", err)
		}
		cmdState := newCommandState()
		username := interactiveGetUser()
		cmdState.rmUser(store, username)

	case cmdModUser.FullCommand():
		store, err := openStorage()
		if err != nil {
			log.Fatalf("Failed to open the storage database: %v", err)
		}
		cmdState := newCommandState()
		username := interactiveGetUser()
		cmdState.modUser(store, username, *flagModUserNewQuota, *flagModUserNewName, *flagModUserNewPass)

	case cmdGetFiles.FullCommand():
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()

		err := cmdState.authenticate(*argGetFilesHost, username, password)
		if err != nil {
			log.Fatalf("Failed to authenticate to the server %s: %v", *argGetFilesHost, err)
		}
		allFiles, err := cmdState.getAllFileHashes()
		if err != nil {
			log.Fatalf("Failed to get all of the files for the user %s from the storage server %s: %v", username, *argGetFilesHost, err)
		}

		// TODO: Better formmating
		log.Printf("All files: %v", allFiles)

	case cmdAddFile.FullCommand():
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()
		err := cmdState.authenticate(*argAddFileHost, username, password)
		if err != nil {
			log.Fatalf("Failed to authenticate to the server %s: %v", *argAddFileHost, err)
		}

		filepath := *argAddFilePath
		remoteTarget := *argAddFileTarget
		if len(remoteTarget) < 1 {
			remoteTarget = filepath
		}
		chunkCount, lastMod, permissions, hashString, err := filefreezer.CalcFileHashInfo(cmdState.serverCapabilities.ChunkSize, filepath)

		if err != nil {
			log.Fatalf("Failed to calculate the required data for the file %s: %v", filepath, err)
		}

		fileInfo, err := cmdState.addFile(filepath, remoteTarget, false, permissions, lastMod, chunkCount, hashString)
		if err != nil {
			log.Fatalf("Failed to register the file on the server %s: %v", *argAddFileHost, err)
		}

		log.Printf("File added (FileId: %d | VersionID: %d): %s\n", fileInfo.FileID, fileInfo.CurrentVersion.VersionID, filepath)

	case cmdRmFile.FullCommand():
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()
		err := cmdState.authenticate(*argRmFileHost, username, password)
		if err != nil {
			log.Fatalf("Failed to authenticate to the server %s: %v", *argAddFileHost, err)
		}

		filepath := *argRmFilePath
		err = cmdState.rmFile(filepath)
		if err != nil {
			log.Fatalf("Failed to remove file from the server %s: %v", *argRmFileHost, err)
		}

	case cmdSync.FullCommand():
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()
		err := cmdState.authenticate(*argSyncHost, username, password)
		if err != nil {
			log.Fatalf("Failed to authenticate to the server %s: %v", *argSyncHost, err)
		}

		filepath := *argSyncPath
		remoteFilepath := *argSyncTarget
		if len(remoteFilepath) < 1 {
			remoteFilepath = filepath
		}
		_, _, err = cmdState.syncFile(filepath, remoteFilepath)
		if err != nil {
			log.Fatalf("Failed to synchronize the path %s: %v", filepath, err)
		}

	case cmdSyncDir.FullCommand():
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()
		err := cmdState.authenticate(*argSyncDirHost, username, password)
		if err != nil {
			log.Fatalf("Failed to authenticate to the server %s: %v", *argSyncDirHost, err)
		}

		filepath := *argSyncDirPath
		remoteFilepath := *argSyncDirTarget
		if len(remoteFilepath) < 1 {
			remoteFilepath = filepath
		}
		_, err = cmdState.syncDirectory(filepath, remoteFilepath)
		if err != nil {
			log.Fatalf("Failed to synchronize the directory %s: %v", filepath, err)
		}

	case cmdUserStats.FullCommand():
		cmdState := newCommandState()
		username := interactiveGetUser()
		password := interactiveGetPassword()
		err := cmdState.authenticate(*argUserStatsHost, username, password)
		if err != nil {
			log.Fatalf("Failed to authenticate to the server %s: %v", *argUserStatsHost, err)
		}

		_, err = cmdState.getUserStats()
		if err != nil {
			log.Fatalf("Failed to get the user stats from the server %s: %v", *argUserStatsHost, err)
		}

	}
}

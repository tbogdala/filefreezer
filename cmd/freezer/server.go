// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/labstack/echo"
	"github.com/tbogdala/filefreezer"
)

// serverState represents the server state and includes configuration flags.
type serverState struct {
	// DatabasePath is the file path to the database used for storage
	DatabasePath string

	// DefaultQuota is the default quota size for a user
	DefaultQuota int

	// Port is the port to listen to
	Port int

	// Storage is the filefreezer storage object used to keep data
	Storage *filefreezer.Storage

	// JWTSecretBytes is the slice used to authenticate JWT tokens for this
	// server instance.
	JWTSecretBytes []byte
}

// newState does the setup for the initial state of the server
func newState() (*serverState, error) {
	var err error
	s := new(serverState)
	s.DatabasePath = *flagDatabasePath

	// attempt to open the storage database
	s.Storage, err = openStorage()
	if err != nil {
		return nil, fmt.Errorf("Failed to open the database using the path specified (%s): %v", s.DatabasePath, err)
	}

	// generate a random passphrase for signing JWT if something wasn't specified
	// on the command line as a flag; this will make the tokens only
	// valid between the same running instance of the server
	randomPassphrase := []byte(*flagCryptoPass)
	if len(randomPassphrase) < 1 {
		var randoms [32]byte
		_, err = rand.Read(randoms[:])
		if err != nil {
			return nil, fmt.Errorf("A crypto passrandomPassphraseword was not supplied and random generation failed: %v", err)
		}
		logPrintln("JWT random passphrase generated.")
	}
	s.JWTSecretBytes = randomPassphrase

	logPrintf("Database opened: %s\n", s.DatabasePath)
	return s, nil
}

// close will close any state connections used by the server
func (state *serverState) close() {
	state.Storage.Close()
}

func (state *serverState) serve(readyCh chan bool) {
	e := echo.New()
	InitRoutes(state, e)

	// attempt to listen to the interrupt signal to signal the stop
	// chan in a goroutine to call server shutdown.
	// NOTE: doesn't appear to work on windows
	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logPrintln("Shutting down server...")
		if err := e.Shutdown(ctx); err != nil {
			log.Fatalf("could not shutdown: %v", err)
		}
	}()

	// create the HTTP server
	go func() {
		if len(*flagTLSCrt) < 1 || len(*flagTLSKey) < 1 {
			logPrintf("Starting http server on %s ...", *argServeListenAddr)
			if err := e.Start(*argServeListenAddr); err != nil {
				logPrintln("Shutting down the server ...")
			}
		} else {
			logPrintf("Starting https server on %s ...", *argServeListenAddr)
			if err := e.StartTLS(*argServeListenAddr, *flagTLSCrt, *flagTLSKey); err != nil {
				logPrintln("Shutting down the server ...")
			}
		}
	}()

	// now that the listener is up, send out the ready signal
	if readyCh != nil {
		readyCh <- true
	}
}

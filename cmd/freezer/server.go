// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/tbogdala/filefreezer"
	"golang.org/x/net/http2"
)

// serverState represents the server state and includes configuration flags.
type serverState struct {
	// DatabasePath is the file path to the database used for storage
	DatabasePath string

	// DefaultQuota is the default quota size for a user
	DefaultQuota int

	// Port is the port to listen to
	Port int

	// PublicKeyPath is the file path to the public crypto key
	PublicKeyPath string

	// PrivateKeyPath is the file path to the private crypto key
	PrivateKeyPath string

	// SignKey is the loaded crypto key for signing security tokens
	SignKey []byte

	// VerifyKey is the loaded crypto key for verifying security tokens
	VerifyKey []byte

	// Storage is the filefreezer storage object used to keep data
	Storage *filefreezer.Storage

	// Authorizor is the interface able to verify username and passwords
	// as well as sign username and ids into a authentication token.
	Authorizor
}

// newState does the setup for the initial state of the server
func newState() (*serverState, error) {
	s := new(serverState)
	s.PrivateKeyPath = *flagPrivateKeyPath
	s.PublicKeyPath = *flagPublicKeyPath
	s.DatabasePath = *flagDatabasePath

	// load the private key
	var err error
	if s.PrivateKeyPath != "" {
		s.SignKey, err = ioutil.ReadFile(s.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read the private key (%s). %v", s.PrivateKeyPath, err)
		}
	}

	// load the public key
	if s.PublicKeyPath != "" {
		s.VerifyKey, err = ioutil.ReadFile(s.PublicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read the public key (%s). %v", s.PublicKeyPath, err)
		}
	}

	// attempt to open the storage database
	s.Storage, err = openStorage()
	if err != nil {
		return nil, fmt.Errorf("Failed to open the database using the path specified (%s): %v", s.DatabasePath, err)
	}

	// assign the token generator
	s.Authorizor, err = NewJWTAuthenticator(s.Storage, s.SignKey, s.VerifyKey)
	if err != nil {
		return nil, fmt.Errorf("Failed to create the JWT token generator: %v", err)
	}

	log.Printf("Database opened: %s\n", s.DatabasePath)
	return s, nil
}

// close will close any state connections used by the server
func (state *serverState) close() {
	state.Storage.Close()
}

func (state *serverState) serve(readyCh chan bool) {
	// create the HTTP server
	routes := InitRoutes(state)
	httpServer := &http.Server{
		Addr:    *argServeListenAddr,
		Handler: routes,
	}

	// attempt to listen to the interrupt signal to signal the stop
	// chan in a goroutine to call server shutdown.
	// NOTE: doesn't appear to work on windows
	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		d := time.Now().Add(5 * time.Second) // deadline 5s max
		ctx, cancel := context.WithDeadline(context.Background(), d)
		defer cancel()
		log.Println("Shutting down server...")
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Fatalf("could not shutdown: %v", err)
		}
	}()

	// now that the listener is up, send out the ready signal
	if readyCh != nil {
		readyCh <- true
	}

	var err error
	if len(*flagTLSCrt) < 1 || len(*flagTLSKey) < 1 {
		log.Printf("Starting http server on %s ...", *argServeListenAddr)
		err = httpServer.ListenAndServe()
	} else {
		log.Printf("Starting https server on %s ...", *argServeListenAddr)
		err = http2.ConfigureServer(httpServer, nil)
		if err != nil {
			log.Printf("Unable to enable HTTP/2 for the server: %v", err)
		}
		err = httpServer.ListenAndServeTLS(*flagTLSCrt, *flagTLSKey)
	}
	if err != nil && err != http.ErrServerClosed {
		log.Printf("There was an error while running the HTTP server: %v", err)
	}
}

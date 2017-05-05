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
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
	"github.com/tbogdala/filefreezer/cmd/freezer/routes"
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

// runAddUser adds a user to the database
func runAddUser(username string, password string, quota int) {
	store, err := openStorage()
	if err != nil {
		log.Fatalf("Failed to open the storage database: %v", err)
	}

	// generate the salt and salted password hash
	salt, saltedPass, err := filefreezer.GenSaltedHash(password)
	if err != nil {
		log.Fatalf("Failed to generate a password hash %v", err)
	}

	// add the user to the database
	_, err = store.AddUser(username, salt, saltedPass, quota)
	if err != nil {
		log.Fatalf("Failed to create the user %s: %v", username, err)
	}

	log.Println("User created successfully")
}

// runModUser modifies a user in the database
func runModUser(username string, password string, quota int) {
	store, err := openStorage()
	if err != nil {
		log.Fatalf("Failed to open the storage database: %v", err)
	}

	// get existing user
	user, err := store.GetUser(username)
	if err != nil {
		log.Fatalf("Failed to get an existing user with the name %s: %v", username, err)
	}

	// generate the salt and salted password hash
	salt, saltedPass, err := filefreezer.GenSaltedHash(password)
	if err != nil {
		log.Fatalf("Failed to generate a password hash %v", err)
	}

	// add the user to the database
	err = store.UpdateUser(user.ID, salt, saltedPass, quota)
	if err != nil {
		log.Fatalf("Failed to modify the user %s: %v", username, err)
	}

	log.Println("User modified successfully")
}

func runGetAllFileHashes() {

}

func runServe(dbPath string, publicKeyPath string, privateKeyPath string) {
	// newState does the setup for the initial state of the server
	newState := func() (*models.State, error) {
		s := new(models.State)
		s.PrivateKeyPath = privateKeyPath
		s.PublicKeyPath = publicKeyPath
		s.DatabasePath = dbPath

		// load the private key
		var err error
		s.SignKey, err = ioutil.ReadFile(s.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read the private key (%s). %v", s.PrivateKeyPath, err)
		}

		// load the public key
		s.VerifyKey, err = ioutil.ReadFile(s.PublicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read the public key (%s). %v", s.PublicKeyPath, err)
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

	// closeState will close any state connections used by the server
	closeState := func(s *models.State) {
		s.Storage.Close()
	}

	// setup a new server state or exit out on failure
	state, err := newState()
	if err != nil {
		log.Fatalf("Unable to initialize the server: %v", err)
	}
	defer closeState(state)

	// create the HTTP server
	routes := routes.InitRoutes(state)
	httpServer := &http.Server{
		Addr:    *argListenAddr,
		Handler: routes,
	}

	// attempt to listen to the interrupt signal to signal the stop
	// chan in a goroutine to call server shutdown.
	// NOTE: doesn't appear to work on windows
	stop := make(chan os.Signal)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		d := time.Now().Add(60 * time.Second) // deadline 5s max
		ctx, cancel := context.WithDeadline(context.Background(), d)
		defer cancel()
		log.Println("Shutting down server...")
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Fatalf("could not shutdown: %v", err)
		}
	}()

	log.Printf("Starting http server on %s ...", *argListenAddr)
	err = httpServer.ListenAndServe()
}

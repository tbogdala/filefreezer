// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/tbogdala/filefreezer/cmd/freezer/models"
	"golang.org/x/net/http2"
)

func runServe(state *models.State, readyCh chan bool) {
	// create the HTTP server
	routes := InitRoutes(state)
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
		log.Printf("Starting http server on %s ...", *argListenAddr)
		err = httpServer.ListenAndServe()
	} else {
		log.Printf("Starting https server on %s ...", *argListenAddr)
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

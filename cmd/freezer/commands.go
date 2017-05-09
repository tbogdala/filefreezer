// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"encoding/json"
	"io/ioutil"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// openStorage is the common function used to open the filefreezer Storage
func openStorage() (*filefreezer.Storage, error) {
	log.Printf("Opening database: %s\n", *flagDatabasePath)

	// open up the storage database
	store, err := filefreezer.NewStorage(*flagDatabasePath)
	if err != nil {
		return nil, err
	}
	store.ChunkSize = *flagChunkSize
	store.CreateTables()
	return store, nil
}

// runAddUser adds a user to the database
func runAddUser(store *filefreezer.Storage, username string, password string, quota int) *filefreezer.User {
	// generate the salt and salted password hash
	salt, saltedPass, err := filefreezer.GenSaltedHash(password)
	if err != nil {
		log.Fatalf("Failed to generate a password hash %v", err)
	}

	// add the user to the database
	user, err := store.AddUser(username, salt, saltedPass, quota)
	if err != nil {
		log.Fatalf("Failed to create the user %s: %v", username, err)
	}

	log.Println("User created successfully")
	return user
}

// runModUser modifies a user in the database
func runModUser(store *filefreezer.Storage, username string, password string, quota int) {
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

// runUserAuthenticate will use a HTTP call to authenticate the user
// and return the JWT token string.
func runUserAuthenticate(hostURI, username, password string) (string, error) {
	// Build and perform the request
	target := fmt.Sprintf("%s/api/users/login", hostURI)
	resp, err := http.PostForm(target, url.Values{
		"user":     {username},
		"password": {password},
	})
	if err != nil {
		if resp != nil {
			return "", fmt.Errorf("Failed to make the HTTP POST request to %s (status: %s): %v", target, resp.Status, err)
		}
		return "", fmt.Errorf("Failed to make the HTTP POST request to %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Failed to read the response body from %s: %v", target, err)
	}

	// check the status code to ensure the success of the call
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Failed to make the HTTP POST request to %s (status: %s): %v", target, resp.Status, string(body))
	}

	// get the response by deserializing the JSON
	var userLogin UserLoginResponse
	err = json.Unmarshal(body, &userLogin)
	if err != nil {
		return "", fmt.Errorf("Poorly formatted response to %s: %v", target, err)
	}

	return userLogin.Token, nil
}

// buildAuthRequest builds a http client and request with the authorization header and token attached.
func buildAuthRequest(target string, method string, token string, bodyBytes []byte) (*http.Client, *http.Request) {
	client := &http.Client{}

	var req *http.Request
	if bodyBytes != nil {
		req, _ = http.NewRequest(method, target, bytes.NewBuffer(bodyBytes))
	} else {
		req, _ = http.NewRequest(method, target, nil)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	return client, req
}

// runAuthRequest will build the http client and request then get the response and read
// the body into a byte array.
func runAuthRequest(target string, method string, token string, reqBody interface{}) ([]byte, error) {
	// serialize the reqBody object if one was passed in
	var err error
	var reqBytes []byte
	if reqBody != nil {
		reqBytes, err = json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("Failed to JSON serialize the data object passed in: %v", err)
		}
	}

	client, req := buildAuthRequest(target, method, token, reqBytes)

	// set the header if a JSON object is being sent
	if reqBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// perform the request and read the response body
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Failed to make the HTTP %s request to %s (status: %s): %v", method, target, resp.Status, err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read the response body from %s: %v", target, err)
	}

	// check the status code to ensure the success of the call
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to make the HTTP %s request to %s (status: %s): %v", method, target, resp.Status, string(body))
	}

	return body, nil
}

func runGetAllFileHashes(hostURI, token string) ([]filefreezer.FileInfo, error) {
	target := fmt.Sprintf("%s/api/files", hostURI)
	body, err := runAuthRequest(target, "GET", token, nil)
	if err != nil {
		return nil, err
	}

	var allFiles AllFilesGetResponse
	err = json.Unmarshal(body, &allFiles)
	if err != nil {
		return nil, fmt.Errorf("Poorly formatted response to %s: %v", target, err)
	}

	return allFiles.Files, nil
}

func runAddFile(hostURI string, token string, fileName string, lastMod int64, chunkCount int, fileHash string) (int, error) {
	var putReq FilePutRequest
	putReq.FileName = fileName
	putReq.LastMod = lastMod
	putReq.ChunkCount = chunkCount
	putReq.FileHash = fileHash

	target := fmt.Sprintf("%s/api/files", hostURI)
	body, err := runAuthRequest(target, "POST", token, putReq)
	if err != nil {
		return 0, err
	}

	var putResp FilePutResponse
	err = json.Unmarshal(body, &putResp)
	if err != nil {
		return 0, err
	}
	return putResp.FileID, nil
}

func runRmFile(hostURI string, token string, filename string) error {
	var getReq FileGetByNameRequest
	getReq.FileName = filename

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/name", hostURI)
	body, err := runAuthRequest(target, "GET", token, getReq)
	var fi FileGetResponse
	err = json.Unmarshal(body, &fi)
	if err != nil {
		return fmt.Errorf("Failed to get the file information for the file name given (%s): %v", filename, err)
	}

	target = fmt.Sprintf("%s/api/file/%d", hostURI, fi.FileID)
	body, err = runAuthRequest(target, "DELETE", token, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file %s: %v", filename, err)
	}

	log.Printf("Removed file: %s\n", filename)

	return nil
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used so dead TCP connections go away.
// Source: https://golang.org/src/net/http/server.go
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}

	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}

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

	addr := httpServer.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Failed to create the server listening socket: %v", err)
		return
	}

	// now that the listener is up, send out the ready signal
	log.Printf("Starting http server on %s ...", addr)
	if readyCh != nil {
		readyCh <- true
	}

	err = httpServer.Serve(tcpKeepAliveListener{ln.(*net.TCPListener)})
	if err != nil && err != http.ErrServerClosed {
		log.Printf("There was an error while running the HTTP server: %v", err)
	}
}

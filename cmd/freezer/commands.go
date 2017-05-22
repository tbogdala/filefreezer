// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/net/http2"

	"encoding/base64"
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

// runModUser modifies a user in the database. if the newQuota, newUsername or newPassword
// fields are non-nil then their values are updated in the database.
func runModUser(store *filefreezer.Storage, username string, newQuota int, newUsername string, newPassword string) {
	// get existing user
	user, err := store.GetUser(username)
	if err != nil {
		log.Fatalf("Failed to get an existing user with the name %s: %v", username, err)
	}
	stats, err := store.GetUserStats(user.ID)
	if err != nil {
		log.Fatalf("Failed to get an existing user stats with the name %s: %v", username, err)
	}

	updatedName := user.Name
	if newUsername != "" {
		updatedName = newUsername
	}

	updatedSalt := user.Salt
	updatedSaltedHash := user.SaltedHash
	if newPassword != "" {
		updatedSalt, updatedSaltedHash, err = filefreezer.GenSaltedHash(newPassword)
		if err != nil {
			log.Fatalf("Failed to generate a password hash %v", err)
		}
	}

	updatedQuota := stats.Quota
	if newQuota > 0 {
		updatedQuota = newQuota
	}

	// update the user in the database
	err = store.UpdateUser(user.ID, updatedName, updatedSalt, updatedSaltedHash, updatedQuota)
	if err != nil {
		log.Fatalf("Failed to modify the user %s: %v", username, err)
	}

	log.Println("User modified successfully")
}

// runUserAuthenticate will use a HTTP call to authenticate the user
// and return the JWT token string.
func runUserAuthenticate(hostURI, username, password string) (string, error) {
	// get the http client to use for the connection
	client, err := getHTTPClient()
	if err != nil {
		return "", err
	}

	// Build and perform the request
	target := fmt.Sprintf("%s/api/users/login", hostURI)
	resp, err := client.PostForm(target, url.Values{
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

// getHttpClient returns a new http Client object set to work with TLS if keys are provided
// on the command line or plain http otherwise.
func getHTTPClient() (*http.Client, error) {
	var client *http.Client
	if *flagTLSCrt != "" && *flagTLSKey != "" {
		cert, err := tls.LoadX509KeyPair(*flagTLSCrt, *flagTLSKey)
		if err != nil {
			return nil, fmt.Errorf("unable to load cert: %v", err)
		}

		xpool := x509.NewCertPool()
		tlsConfig := &tls.Config{
			RootCAs:      xpool,
			Certificates: []tls.Certificate{cert},
		}
		//tlsConfig.BuildNameToCertificate()
		transport := &http2.Transport{TLSClientConfig: tlsConfig}
		client = &http.Client{Transport: transport}

		// Load our trusted certificate path
		certPath := *flagTLSCrt
		pemData, err := ioutil.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to load the certificate file %s: %v", certPath, err)
		}
		ok := tlsConfig.RootCAs.AppendCertsFromPEM(pemData)
		if !ok {
			return nil, fmt.Errorf("couldn't load PEM data for HTTPS client")
		}
	} else {
		client = &http.Client{}
	}

	return client, nil
}

// buildAuthRequest builds a http client and request with the authorization header and token attached.
func buildAuthRequest(target string, method string, token string, bodyBytes []byte) (*http.Client, *http.Request, error) {
	// Load client cert
	client, err := getHTTPClient()
	if err != nil {
		return nil, nil, err
	}

	var req *http.Request
	if bodyBytes != nil {
		req, _ = http.NewRequest(method, target, bytes.NewBuffer(bodyBytes))
	} else {
		req, _ = http.NewRequest(method, target, nil)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	return client, req, nil
}

// runAuthRequest will build the http client and request then get the response and read
// the body into a byte array. If reqBody is a []byte array, no transformation is done,
// but if it's another type than it gets marshalled to a text JSON object.
func runAuthRequest(target string, method string, token string, reqBody interface{}) ([]byte, error) {
	// serialize the reqBody object if one was passed in
	var err error
	var reqBodyIsByteSlice bool
	var reqBytes []byte
	if reqBody != nil {
		reqBytes, reqBodyIsByteSlice = reqBody.([]byte)
		if !reqBodyIsByteSlice {
			reqBytes, err = json.Marshal(reqBody)
			if err != nil {
				return nil, fmt.Errorf("Failed to JSON serialize the data object passed in: %v", err)
			}
		}
	}

	client, req, err := buildAuthRequest(target, method, token, reqBytes)
	if err != nil {
		return nil, err
	}

	// set the header if a JSON object is being sent
	if reqBytes != nil && !reqBodyIsByteSlice {
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

	var allFiles models.AllFilesGetResponse
	err = json.Unmarshal(body, &allFiles)
	if err != nil {
		return nil, fmt.Errorf("Poorly formatted response to %s: %v", target, err)
	}

	return allFiles.Files, nil
}

func runAddFile(hostURI string, token string, fileName string, remoteFilepath string, lastMod int64, chunkCount int, fileHash string) (int, error) {
	var putReq models.FilePutRequest
	putReq.FileName = remoteFilepath
	putReq.LastMod = lastMod
	putReq.ChunkCount = chunkCount
	putReq.FileHash = fileHash

	target := fmt.Sprintf("%s/api/files", hostURI)
	body, err := runAuthRequest(target, "POST", token, putReq)
	if err != nil {
		return 0, err
	}

	// if the POST fails or the response is bad, then the file wasn't registered
	// with the freezer, so there's nothing to rollback -- just return.
	var putResp models.FilePutResponse
	err = json.Unmarshal(body, &putResp)
	if err != nil {
		return 0, err
	}

	// we've registered the file, so now we should sync it
	_, _, err = runSyncFile(hostURI, token, fileName, remoteFilepath)

	return putResp.FileID, err
}

const (
	syncStatusMissing     = 1
	syncStatusLocalNewer  = 2
	syncStatusRemoteNewer = 3
	syncStatusSame        = 4
)

func runSyncFile(hostURI string, token string, localFilename string, remoteFilepath string) (status int, changeCount int, e error) {
	var getReq models.FileGetByNameRequest
	var remote models.FileGetResponse

	// get the file information for the filename, which provides
	// all of the information necessary to determine what to sync.
	getReq.FileName = remoteFilepath
	target := fmt.Sprintf("%s/api/file/name", hostURI)
	body, err := runAuthRequest(target, "GET", token, getReq)

	// if the file is not registered with the storage server, then upload it ...
	// futher checking will be unnecessary.
	if err != nil {
		localChunkCount, localLastMod, localHash, err := filefreezer.CalcFileHashInfo(*flagChunkSize, localFilename)
		if err != nil {
			return syncStatusMissing, 0, fmt.Errorf("Failed to calculate the file hash data for %s: %v", localFilename, err)
		}
		ulCount, err := syncUpload(hostURI, token, localFilename, remoteFilepath, localLastMod, localChunkCount, localHash)
		if err != nil {
			return syncStatusMissing, ulCount, fmt.Errorf("Failed to upload the file to the server %s: %v", hostURI, err)
		}
		return syncStatusLocalNewer, ulCount, nil
	}

	// we got a valid response so the file is registered on the server;
	// continue checking...
	err = json.Unmarshal(body, &remote)
	if err != nil {
		return 0, 0, fmt.Errorf("Failed to get the file information for the file name given (%s): %v", remoteFilepath, err)
	}

	// if the local file doesn't exist then download the file from the server if
	// it is registered there.
	if _, err := os.Stat(localFilename); os.IsNotExist(err) {
		dlCount, err := syncDownload(hostURI, token, remote.FileID, localFilename, remoteFilepath, remote.ChunkCount)
		return syncStatusRemoteNewer, dlCount, err
	}

	// calculate some of the local file information
	localChunkCount, localLastMod, localHash, err := filefreezer.CalcFileHashInfo(*flagChunkSize, localFilename)
	if err != nil {
		return 0, 0, fmt.Errorf("Failed to calculate the file hash data for %s: %v", localFilename, err)
	}

	// lets prove that we don't need to do anything for some cases
	// NOTE: a lastMod difference here doesn't trigger a difference if other metrics check out the same
	if localHash == remote.FileHash && len(remote.MissingChunks) == 0 && localChunkCount == remote.ChunkCount {
		different := false
		if *flagExtraStrict {
			// now we get a chunk list for the file
			var remoteChunks models.FileChunksGetResponse
			target := fmt.Sprintf("%s/api/chunk/%d", hostURI, remote.FileID)
			body, err := runAuthRequest(target, "GET", token, nil)
			err = json.Unmarshal(body, &remoteChunks)
			if err != nil {
				return 0, 0, fmt.Errorf("Failed to get the file chunk list for the file name given (%s): %v", remoteFilepath, err)
			}

			// sanity check
			remoteChunkCount := len(remoteChunks.Chunks)
			if localChunkCount == remoteChunkCount {
				// check the local chunks against remote hashes
				err = forEachChunk(int(*flagChunkSize), localFilename, localChunkCount, func(i int, b []byte) (bool, error) {
					// hash the chunk
					hasher := sha1.New()
					hasher.Write(b)
					hash := hasher.Sum(nil)
					chunkHash := base64.URLEncoding.EncodeToString(hash)

					// do the hashes match?
					if strings.Compare(chunkHash, remoteChunks.Chunks[i].ChunkHash) != 0 {
						// FIXME: At this point we have a chunk difference and it should be left to
						// the client as to which source to trust for the correct file, local or remote.
						different = true
						return false, nil
					}
					return true, nil
				})
				if err != nil {
					return 0, 0, fmt.Errorf("Failed to check the local file (%s) against the remote hashes: %v", localFilename, err)
				}
			}
		}

		// after whole-file hashs and all chunk hashs match, we can feel safe in saying they're not different
		if !different {
			log.Printf("%s --- unchanged", remoteFilepath)
			return syncStatusSame, 0, nil
		}
	}

	// at this point we have a file difference. we'll use the local file as the source of truth
	// if it's lastMod is newer than the remote file.
	if localLastMod > remote.LastMod {
		ulCount, e := syncUploadNewer(hostURI, token, remote.FileID, localFilename, remoteFilepath, localLastMod, localChunkCount, localHash)
		return syncStatusLocalNewer, ulCount, e
	}

	if localLastMod < remote.LastMod {
		dlCount, e := syncDownload(hostURI, token, remote.FileID, localFilename, remoteFilepath, remote.ChunkCount)
		return syncStatusRemoteNewer, dlCount, e
	}

	// there's been a difference detected in the files, but the mod times were the same, so
	// we attempt to upload any missing chunks.
	if len(remote.MissingChunks) > 0 {
		ulCount, e := syncUploadMissing(hostURI, token, remote.FileID, localFilename, remoteFilepath, localChunkCount)
		return syncStatusMissing, ulCount, e
	}

	// we checked to make sure it was the same above, but we found it different -- however, no steps to
	// resolve this were taken, so through an error.
	return 0, 0, fmt.Errorf("found differences between local (%s) and remote (%s) versions, but this was not reconcilled", localFilename, remoteFilepath)
}

func syncUploadMissing(hostURI string, token string, remoteID int, filename string, remoteFilepath string, localChunkCount int) (uploadCount int, e error) {
	// upload each chunk
	err := forEachChunk(int(*flagChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		target := fmt.Sprintf("%s/api/chunk/%d/%d/%s", hostURI, remoteID, i, chunkHash)
		body, err := runAuthRequest(target, "PUT", token, b)
		if err != nil {
			return false, err
		}

		var resp models.FileChunkPutResponse
		err = json.Unmarshal(body, &resp)
		if err != nil || resp.Status == false {
			return false, fmt.Errorf("Failed to upload the chunk to the server: %v", err)
		}

		log.Printf("%s +++ %d / %d", remoteFilepath, i+1, localChunkCount)
		uploadCount++

		return true, nil
	})
	if err != nil {
		return uploadCount, fmt.Errorf("Failed to upload the local file chunk for %s: %v", filename, err)
	}

	return uploadCount, nil
}

func syncUploadNewer(hostURI string, token string, remoteFileID int, filename string, remoteFilepath string,
	localLastMod int64, localChunkCount int, localHash string) (uploadCount int, e error) {
	// delete the remote file
	target := fmt.Sprintf("%s/api/file/%d", hostURI, remoteFileID)
	_, err := runAuthRequest(target, "DELETE", token, nil)
	if err != nil {
		return 0, fmt.Errorf("Failed to remove the file %d: %v", remoteFileID, err)
	}
	log.Printf("%s XXX deleted remote", filename)

	return syncUpload(hostURI, token, filename, remoteFilepath, localLastMod, localChunkCount, localHash)
}

func syncUpload(hostURI string, token string, filename string, remoteFilepath string, localLastMod int64, localChunkCount int, localHash string) (uploadCount int, e error) {
	// establish a new file on the remote freezer
	var putReq models.FilePutRequest
	putReq.FileName = remoteFilepath
	putReq.LastMod = localLastMod
	putReq.ChunkCount = localChunkCount
	putReq.FileHash = localHash
	target := fmt.Sprintf("%s/api/files", hostURI)
	body, err := runAuthRequest(target, "POST", token, putReq)
	if err != nil {
		return 0, err
	}

	var putResp models.FilePutResponse
	err = json.Unmarshal(body, &putResp)
	if err != nil {
		return 0, err
	}
	remoteID := putResp.FileID

	// upload each chunk
	err = forEachChunk(int(*flagChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		target = fmt.Sprintf("%s/api/chunk/%d/%d/%s", hostURI, remoteID, i, chunkHash)
		body, err = runAuthRequest(target, "PUT", token, b)
		if err != nil {
			return false, err
		}

		var resp models.FileChunkPutResponse
		err = json.Unmarshal(body, &resp)
		if err != nil || resp.Status == false {
			return false, fmt.Errorf("Failed to upload the chunk to the server: %v", err)
		}

		log.Printf("%s >>> %d / %d", remoteFilepath, i+1, localChunkCount)
		uploadCount++

		return true, nil
	})
	if err != nil {
		return uploadCount, fmt.Errorf("Failed to upload the local file chunk for %s: %v", filename, err)
	}

	log.Printf("%s ==> uploaded", remoteFilepath)
	return uploadCount, nil
}

func syncDownload(hostURI string, token string, remoteID int, filename string, remoteFilepath string, chunkCount int) (downloadCount int, e error) {
	localFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return 0, fmt.Errorf("Failed to open local file (%s) for writing: %v", filename, err)
	}
	defer localFile.Close()

	// download each chunk and write it out to the file
	chunksWritten := 0
	for i := 0; i < chunkCount; i++ {
		target := fmt.Sprintf("%s/api/chunk/%d/%d", hostURI, remoteID, i)
		body, err := runAuthRequest(target, "GET", token, nil)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to get the file chunk #%d for file id%d: %v", i, remoteID, err)
		}

		var chunkResp models.FileChunkGetResponse
		err = json.Unmarshal(body, &chunkResp)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to get the file chunk #%d for file id%d: %v", i, remoteID, err)
		}

		// trim the buffer at the EOF marker of byte(0)
		chunk := chunkResp.Chunk.Chunk
		eofIndex := bytes.IndexByte(chunk, byte(0))
		if eofIndex > 0 && eofIndex < len(chunk) {
			chunk = chunk[:eofIndex]
		}

		_, err = localFile.Write(chunk)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to write to the #%d chunk to the local file %s: %v", i, filename, err)
		}

		log.Printf("%s <<< %d / %d", remoteFilepath, i+1, chunkCount)
		chunksWritten++
	}

	log.Printf("%s <== downloaded", remoteFilepath)
	return chunksWritten, nil
}

type eachChunkFunc func(chunkNumber int, chunk []byte) (bool, error)

func forEachChunk(chunkSize int, filename string, localChunkCount int, eachFunc eachChunkFunc) error {
	// open the local file and create a chunk sized buffer
	buffer := make([]byte, chunkSize)
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("Failed to open the file %s: %v", filename, err)
	}
	defer f.Close()

	// with the chunk list, lets make sure that each chunk locally has the same hash
	for i := 0; i < localChunkCount; i++ {
		readCount, err := io.ReadAtLeast(f, buffer, chunkSize)
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				// if we don't fill the buffer and we're not on the last chunk, the files are different
				if i+1 != localChunkCount {
					return fmt.Errorf("nexpeced EOF while reading the file %s", filename)
				}
			} else {
				return fmt.Errorf("an error occured while reading %d bytes from the file %s: %v", readCount, filename, err)
			}
		}
		clampedBuffer := buffer[:readCount]

		// call the supplied callback and break the loop if false is returned
		contLoop, err := eachFunc(i, clampedBuffer)
		if err != nil {
			return err
		}
		if !contLoop {
			break
		}
	}

	return nil
}

func runRmFile(hostURI string, token string, filename string) error {
	var getReq models.FileGetByNameRequest
	getReq.FileName = filename

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/name", hostURI)
	body, err := runAuthRequest(target, "GET", token, getReq)
	var fi models.FileGetResponse
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

func runUserStats(hostURI string, token string) (stats filefreezer.UserStats, e error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/user/stats", hostURI)
	body, err := runAuthRequest(target, "GET", token, nil)
	var r models.UserStatsGetResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		e = fmt.Errorf("Failed to get the user stats: %v", err)
		return
	}

	log.Printf("Quota:     %v\n", r.Stats.Quota)
	log.Printf("Allocated: %v\n", r.Stats.Allocated)
	log.Printf("Revision:  %v\n", r.Stats.Revision)

	stats = r.Stats
	return
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

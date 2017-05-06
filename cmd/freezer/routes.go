// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// InitRoutes creates the routing multiplexer for the server
func InitRoutes(state *models.State) *mux.Router {
	// setup the web server routing table
	r := mux.NewRouter().StrictSlash(false)

	// setup the user login handler
	r.Handle("/api/users/login", handleUsersLogin(state)).Methods("POST")

	// returns all files and their whole-file hash
	r.Handle("/api/files", authenticateToken(state, handleGetAllFiles(state))).Methods("GET")

	// handles registering a file to a user
	r.Handle("/api/files", authenticateToken(state, handlePutFile(state))).Methods("POST")

	// returns a file chunk list with hashes
	// /api/file/{id}

	// returns/put a file chunk
	// /api/file/{id}/{chunk num}
	// setup the notepad handlers, hot CRUD style

	return r
}

// handleUsersLogin handles the incoming POST /api/users/login
func handleUsersLogin(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// get the user and password from the parameters
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Failed to parse form data for POST operation.", http.StatusBadRequest)
			return
		}

		vars := r.Form
		username := vars.Get("user")
		password := vars.Get("password")
		if username == "" || password == "" {
			http.Error(w, "Both user and password were not supplied.", http.StatusBadRequest)
			return
		}

		// check the username and password
		user, err := state.Authorizor.VerifyPassword(username, password)
		if err != nil || user == nil {
			http.Error(w, "Failed to log in with the data provided.", http.StatusBadRequest)
			return
		}

		// generate the authentication token
		token, err := state.Authorizor.GenerateToken(user.Name, user.ID)
		if err != nil {
			http.Error(w, "Failed to log in with the data provided.", http.StatusBadRequest)
			return
		}

		writeJSONResponse(w, token)
	}
}

// AllFilesGetResponse is the JSON serializable response given by the
// /api/files GET handlder.
type AllFilesGetResponse struct {
	Files []filefreezer.FileInfo
}

// handleGetAllFiles returns a JSON object with all of the FileInfo objects in Storage
// that are bound to the user id authorized in the context of the call.
func handleGetAllFiles(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull down all the fileinfo objects for a user
		allFileInfos, err := state.Storage.GetAllUserFileInfos(userCreds.ID)
		if err != nil {
			http.Error(w, "Failed to get files for the user", http.StatusNotFound)
			return
		}

		writeJSONResponse(w, &AllFilesGetResponse{
			Files: allFileInfos,
		})
	}
}

type FilePutResponse struct {
	FileID int
}

type FilePutRequest struct {
	FileName   string
	LastMod    int64
	ChunkCount int
	FileHash   string
}

// handlePutFile registers a file for a given user.
func handlePutFile(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// pull the user credentials
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// deserialize the JSON object that should be in the request body
		var req FilePutRequest
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read the request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(body, &req)
		if err != nil {
			http.Error(w, "Failed to parse the request as a JSON object: "+err.Error(), http.StatusBadRequest)
			return
		}

		// sanity check some input
		if len(req.FileName) < 1 {
			http.Error(w, "fileName must be supplied in the request", http.StatusBadRequest)
			return
		}
		if req.LastMod < 1 {
			http.Error(w, "lastMod time must be supplied in the request", http.StatusBadRequest)
			return
		}
		if req.ChunkCount < 1 {
			http.Error(w, "chunkCount must be supplied in the request", http.StatusBadRequest)
			return
		}
		if len(req.FileHash) < 1 {
			http.Error(w, "fileHash must be supplied in the request", http.StatusBadRequest)
			return
		}

		// register a new file in storage with the information
		fi, err := state.Storage.AddFileInfo(userCreds.ID, req.FileName, req.LastMod, req.ChunkCount, req.FileHash)
		if err != nil {
			http.Error(w, "Failed to put a new file in storage for the user. "+err.Error(), http.StatusConflict)
			return
		}

		writeJSONResponse(w, &FilePutResponse{
			fi.FileID,
		})
	}
}

type userCredentialsContextKey string
type userCredentialsContext struct {
	ID   int
	Name string
}

// authenticateToken middleware calls out to the auth module to authenticate
// the token contained in the header of the response to ensure user credentials
// before calling the next handler.
func authenticateToken(state *models.State, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// validate the token
		token, err := state.Authorizor.VerifyToken(r)
		if err != nil || token == nil {
			http.Error(w, "Failed to authenticate.", http.StatusForbidden)
			return
		}
		username, userid := state.Authorizor.GetUserFromToken(token)
		creds := &userCredentialsContext{userid, username}

		// authenticated, so proceed to next handler
		ctx := r.Context()
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userCredentialsContextKey("UserCredentials"), creds)))
	})
}

// writeJSONResponse marshals the generic data object into JSON and then
// writes it out to the ResponseWriter. If the marshalling fails, then
// a 500 response is returned with the error message.
func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	// set the response to be JSON
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// marshal the data
	json, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// write it out
	w.Write(json)
}

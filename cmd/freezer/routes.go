// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"strconv"

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

	// returns a file information response with missing chunk list
	r.Handle("/api/file/{fileid:[0-9]+}", authenticateToken(state, handleGetFile(state))).Methods("GET")

	// returns a file information response with missing chunk list -- same as /api/file/{fileid} but for filenames
	r.Handle("/api/file/name", authenticateToken(state, handleGetFileByName(state))).Methods("GET")

	// deletes a file
	r.Handle("/api/file/{fileid}", authenticateToken(state, handleDeleteFile(state))).Methods("DELETE")

	// put a file chunk
	r.Handle("/api/chunk/{fileid}/{chunknumber}/{chunkhash}", authenticateToken(state, handlePutFileChunk(state))).Methods("PUT")

	// get a file chunk
	r.Handle("/api/chunk/{fileid}/{chunknumber}", authenticateToken(state, handleGetFileChunk(state))).Methods("GET")

	// get all known file chunks (except the chunks themselvesl)
	r.Handle("/api/chunk/{fileid}", authenticateToken(state, handleGetFileChunks(state))).Methods("GET")

	return r
}

// UserLoginResponse is the JSON serializable response given by the
// /api/users/login POST handlder.
type UserLoginResponse struct {
	Token string
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

		writeJSONResponse(w, &UserLoginResponse{token})
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
			http.Error(w, "Failed to get the user credentials.", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull down all the fileinfo objects for a user
		allFileInfos, err := state.Storage.GetAllUserFileInfos(userCreds.ID)
		if err != nil {
			http.Error(w, "Failed to get files for the user.", http.StatusNotFound)
			return
		}

		writeJSONResponse(w, &AllFilesGetResponse{
			Files: allFileInfos,
		})
	}
}

// FileGetResponse is the JSON serializable response given by the
// /api/file/{id} GET handlder.
type FileGetResponse struct {
	filefreezer.FileInfo
	MissingChunks []int
}

// FileGetByNameRequest is the JSON structure to be sent to the
// /api/file/name GET handler.
type FileGetByNameRequest struct {
	FileName string
}

// handleGetFileByName returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFileByName(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials.", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// deserialize the JSON object that should be in the request body
		var req FileGetByNameRequest
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

		// pull down the fileinfo object for a file ID
		fi, err := state.Storage.GetFileInfoByName(userCreds.ID, req.FileName)
		if err != nil {
			http.Error(w, "Failed to get file for the user.", http.StatusNotFound)
			return
		}

		// get all of the missing chunks
		missingChunks, err := state.Storage.GetMissingChunkNumbersForFile(userCreds.ID, fi.FileID)
		if err != nil {
			http.Error(w, "Failed to get the missing chunks for the file.", http.StatusBadRequest)
			return
		}

		writeJSONResponse(w, &FileGetResponse{
			FileInfo:      *fi,
			MissingChunks: missingChunks,
		})
	}
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFile(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials.", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull the file id from the URI matched by the mux
		vars := mux.Vars(r)
		fileID, err := strconv.ParseInt(vars["fileid"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the file id in the URI.", http.StatusBadRequest)
			return
		}

		// pull down the fileinfo object for a file ID
		fi, err := state.Storage.GetFileInfo(userCreds.ID, int(fileID))
		if err != nil {
			http.Error(w, "Failed to get file for the user.", http.StatusNotFound)
			return
		}

		// get all of the missing chunks
		missingChunks, err := state.Storage.GetMissingChunkNumbersForFile(userCreds.ID, fi.FileID)
		if err != nil {
			http.Error(w, "Failed to get the missing chunks for the file.", http.StatusBadRequest)
			return
		}

		writeJSONResponse(w, &FileGetResponse{
			FileInfo:      *fi,
			MissingChunks: missingChunks,
		})
	}
}

// FileChunkPutResponse is the JSON serializable response given by the
// /api/chunk/{id}/{chunknum} PUT handlder.
type FileChunkPutResponse struct {
	Status bool
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handlePutFileChunk(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials.", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull the file id from the URI matched by the mux
		vars := mux.Vars(r)
		fileID, err := strconv.ParseInt(vars["fileid"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the file id in the URI.", http.StatusBadRequest)
			return
		}
		chunkNumber, err := strconv.ParseInt(vars["chunknumber"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the chunk number in the URI.", http.StatusBadRequest)
			return
		}
		chunkHash, okay := vars["chunkhash"]
		if !okay {
			http.Error(w, "A valid string was not used for the chunk hash in the URI.", http.StatusBadRequest)
			return
		}

		// get a byte limited reader, set to the maximum chunk size supported by Storage
		bodyReader := http.MaxBytesReader(w, r.Body, state.Storage.ChunkSize)
		defer bodyReader.Close()
		chunk, err := ioutil.ReadAll(bodyReader)
		if err != nil {
			http.Error(w, "Failed to read the chunk: "+err.Error(), http.StatusBadRequest)
			return
		}

		// AddFileChunk does verify that the user ID owns the fild ID so we don't need
		// to replicate that work here, just add the chunk.
		fc, err := state.Storage.AddFileChunk(userCreds.ID, int(fileID), int(chunkNumber), chunkHash, chunk)
		if err != nil || fc == nil {
			http.Error(w, "Failed to add the chunk to storage: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSONResponse(w, &FileChunkPutResponse{
			Status: true,
		})
	}
}

// FileChunksGetResponse is the JSON serializable response given by the
// /api/chunk/{fileid}/ GET handlder.
type FileChunksGetResponse struct {
	Chunks []filefreezer.FileChunk
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFileChunks(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials.", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull the file id from the URI matched by the mux
		vars := mux.Vars(r)
		fileID, err := strconv.ParseInt(vars["fileid"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the file id in the URI.", http.StatusBadRequest)
			return
		}

		chunks, err := state.Storage.GetFileChunkInfos(userCreds.ID, int(fileID))
		if err != nil {
			http.Error(w, "Failed to get the chunk informations for the file id in the URI.", http.StatusBadRequest)
			return
		}
		writeJSONResponse(w, &FileChunksGetResponse{
			chunks,
		})
	}
}

// FileChunkGetResponse is the JSON serializable response given by the
// /api/chunk/{fileid}/{chunknumber} GET handlder.
type FileChunkGetResponse struct {
	Chunk filefreezer.FileChunk
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFileChunk(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials.", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull the file id from the URI matched by the mux
		vars := mux.Vars(r)
		fileID, err := strconv.ParseInt(vars["fileid"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the file id in the URI.", http.StatusBadRequest)
			return
		}
		chunkNumber, err := strconv.ParseInt(vars["chunknumber"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the chunk number in the URI.", http.StatusBadRequest)
			return
		}

		// get the file info first to ensure ownership
		fi, err := state.Storage.GetFileInfo(userCreds.ID, int(fileID))
		if err != nil {
			http.Error(w, "Failed to get the file information for the file id in the URI.", http.StatusBadRequest)
			return
		}
		if fi.UserID != userCreds.ID {
			http.Error(w, "Access denied.", http.StatusForbidden)
			return
		}

		chunk, err := state.Storage.GetFileChunk(int(fileID), int(chunkNumber))
		if err != nil {
			http.Error(w, "Failed to get the chunk information for the file id and chunk number in the URI.", http.StatusBadRequest)
			return
		}

		writeJSONResponse(w, &FileChunkGetResponse{
			*chunk,
		})
	}
}

// FilePutResponse is the JSON serializable response given by the
// /api/files PUT handlder.
type FilePutResponse struct {
	FileID int
}

// FilePutResponse is the JSON serializable request object sent to the
// /api/files PUT handlder.
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

// FileDeleteRequest is the JSON serializable request object sent to the
// /api/files/{id} DELETE handlder.
type FileDeleteRequest struct {
	FileID int
}

// FileDeleteResponse is the JSON serializable response object from
// /api/file/{id} DELETE handler.
type FileDeleteResponse struct {
	Success bool
}

func handleDeleteFile(state *models.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// pull the user credentials
		ctx := r.Context()
		userCredsI := ctx.Value(userCredentialsContextKey("UserCredentials"))
		if userCredsI == nil {
			http.Error(w, "Failed to get the user credentials", http.StatusUnauthorized)
			return
		}
		userCreds := userCredsI.(*userCredentialsContext)

		// pull the file id from the URI matched by the mux
		vars := mux.Vars(r)
		fileID, err := strconv.ParseInt(vars["fileid"], 10, 32)
		if err != nil {
			http.Error(w, "A valid integer was not used for the file id in the URI.", http.StatusBadRequest)
			return
		}

		// delete a file from storage with the information
		err = state.Storage.RemoveFile(userCreds.ID, int(fileID))
		if err != nil {
			http.Error(w, "Failed to put a new file in storage for the user. "+err.Error(), http.StatusConflict)
			return
		}

		writeJSONResponse(w, &FileDeleteResponse{true})
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

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"io/ioutil"
	"net/http"
	"time"

	"strconv"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

const (
	jwtClaimUserName = "Username"
	jwtClaimUserID   = "UserID"
	jwtContextName   = "JwtToken"
)

type jwtCustomClaims struct {
	Username string `json:"Username"`
	UserID   int    `json:"UserID"`
	jwt.StandardClaims
}

// InitRoutes creates the routing multiplexer for the server
func InitRoutes(state *serverState, e *echo.Echo) {
	// setup the user login handler
	e.POST("/api/users/login", handleUsersLogin(state))

	restricted := e.Group("/api")
	jwtConfig := middleware.JWTConfig{
		Claims:     &jwtCustomClaims{},
		ContextKey: "JwtToken",
		SigningKey: state.JWTSecretBytes,
	}
	restricted.Use(middleware.JWTWithConfig(jwtConfig))

	// returns the authenticated users's current stats such as quota, allocation and revision counts
	restricted.GET("/user/stats", handleGetUserStats(state))

	// updates the user's crypto hash used to verify the user-entered password client-side.
	restricted.PUT("/user/cryptohash", handlePutUserCryptoHash(state))

	// returns all files and their whole-file hash
	restricted.GET("/files", handleGetAllFiles(state))

	// handles registering a file to a user
	restricted.POST("/files", handlePutFile(state))

	// handles registering a new file version for a given file id
	restricted.POST("/file/:fileid/version", handleNewFileVersion(state))

	// returns a file information response with missing chunk list
	restricted.GET("/file/:fileid", handleGetFile(state))

	// handles registering a new file version for a given file id
	restricted.GET("/file/:fileid/versions", handleGetAllFileVersion(state))

	// deletes a file
	restricted.DELETE("/file/:fileid", handleDeleteFile(state))

	// put a file chunk
	restricted.PUT("/chunk/:fileid/:versionID/:chunknumber/:chunkhash", handlePutFileChunk(state))

	// get a file chunk and returns the raw bytes of the encrypted chunk data
	restricted.GET("/chunk/:fileid/:versionID/:chunknumber", handleGetFileChunk(state))

	// get all known file chunks (except the chunks themselves)
	restricted.GET("/chunk/:fileid/:versionID", handleGetFileChunks(state))
}

// handleUsersLogin handles the incoming POST /api/users/login
func handleUsersLogin(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		username := c.FormValue("user")
		password := c.FormValue("password")
		if username == "" || password == "" {
			return c.String(http.StatusBadRequest, "Both user and password were not supplied.")
		}

		// check the username and password
		user, err := state.Storage.GetUser(username)
		if err != nil {
			return c.String(http.StatusUnauthorized, "Could not find user in the database.")
		}

		verified := filefreezer.VerifyLoginPassword(password, user.Salt, user.SaltedHash)
		if !verified {
			return c.String(http.StatusUnauthorized, "Could not verify the user against the stored salted hash.")
		}

		if err != nil || user == nil {
			return c.String(http.StatusUnauthorized, "Failed to log in with the data provided.")
		}

		// Set claims
		claims := &jwtCustomClaims{
			user.Name,
			user.ID,
			jwt.StandardClaims{
				ExpiresAt: time.Now().Add(time.Minute * 15).Unix(),
			},
		}

		// generate the authentication token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

		// Generate encoded token and send it as response.
		t, err := token.SignedString(state.JWTSecretBytes)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, &models.UserLoginResponse{
			Token:      t,
			CryptoHash: user.CryptoHash,
			Capabilities: models.ServerCapabilities{
				ChunkSize: *flagServeChunkSize,
			},
		})
	}
}

// handlePutUserCryptoHash updates a user's crypto hash which can be used to verify a
// client side entered password.
func handlePutUserCryptoHash(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)
		userID := claims.UserID

		// deserialize the JSON object that should be in the request body
		var req models.UserCryptoHashUpdateRequest
		err := c.Bind(&req)
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to read the request body: "+err.Error())
		}

		// set the new crypto hash for the user
		err = state.Storage.UpdateUserCryptoHash(userID, req.CryptoHash)
		if err != nil {
			return c.String(http.StatusInternalServerError, "Failed to update the user's crypto hash information for the authenticated user.")
		}

		return c.JSON(http.StatusOK, &models.UserCryptoHashUpdateResponse{
			Status: true,
		})
	}
}

// handleGetUserStats returns a JSON object with the authenticated user's current
// stats susch as the quota, allocated byte count and current revision number.
func handleGetUserStats(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		stats, err := state.Storage.GetUserStats(claims.UserID)
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to get the user stats information for the authenticated user.")
		}

		return c.JSON(http.StatusOK, &models.UserStatsGetResponse{
			Stats: filefreezer.UserStats{
				Quota:     stats.Quota,
				Allocated: stats.Allocated,
				Revision:  stats.Revision,
			},
		})
	}
}

// handleGetAllFiles returns a JSON object with all of the FileInfo objects in Storage
// that are bound to the user id authorized in the context of the call.
func handleGetAllFiles(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull down all the fileinfo objects for a user
		allFileInfos, err := state.Storage.GetAllUserFileInfos(claims.UserID)
		if err != nil {
			return c.String(http.StatusNotFound, "Failed to get files for the user.")
		}

		return c.JSON(http.StatusOK, &models.AllFilesGetResponse{
			Files: allFileInfos,
		})
	}
}

func handleNewFileVersion(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// deserialize the JSON object that should be in the request body
		var req models.NewFileVersionRequest
		err := c.Bind(&req)
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to read the request body: "+err.Error())
		}

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadGateway, "A valid integer was not used for the file id in the URI.")
		}

		// pull down the fileinfo object for a file ID
		fi, err := state.Storage.GetFileInfo(claims.UserID, int(fileID))
		if err != nil {
			return c.String(http.StatusNotFound, "Failed to get file for the user.")
		}

		// create new file version
		fi, err = state.Storage.TagNewFileVersion(claims.UserID, int(fileID), req.Permissions, req.LastMod, req.ChunkCount, req.FileHash)
		if err != nil {
			return c.String(http.StatusInternalServerError, "Failed to tag a new version of the file for the user: "+err.Error())
		}

		return c.JSON(http.StatusOK, &models.NewFileVersionResponse{
			FileInfo: *fi,
			Status:   true,
		})
	}
}

func handleGetAllFileVersion(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the file id in the URI.")
		}

		// pull down the fileinfo object for a file ID
		fi, err := state.Storage.GetFileInfo(claims.UserID, int(fileID))
		if err != nil {
			return c.String(http.StatusNotFound, "Failed to get file for the user.")
		}

		// get all the versions associated with the file in storage
		versions, err := state.Storage.GetFileVersions(fi.FileID)
		if err != nil {
			return c.String(http.StatusNotFound, "Failed to get file versions for the user.")
		}

		return c.JSON(http.StatusOK, &models.FileGetAllVersionsResponse{
			Versions: versions,
		})
	}
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFile(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the file id in the URI.")
		}

		// pull down the fileinfo object for a file ID
		fi, err := state.Storage.GetFileInfo(claims.UserID, int(fileID))
		if err != nil {
			return c.String(http.StatusNotFound, "Failed to get file for the user.")
		}

		// get all of the missing chunks
		missingChunks, err := state.Storage.GetMissingChunkNumbersForFile(claims.UserID, fi.FileID)
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to get the missing chunks for the file.")
		}

		return c.JSON(http.StatusOK, &models.FileGetResponse{
			FileInfo:      *fi,
			MissingChunks: missingChunks,
		})
	}
}

// handlePutFileChunk reads a chunk from the request body and attempts to store it given the
// file ID, chunk number and hash supplied in parameters. A Status boolean is returned to
// indicate the success of the operation.
func handlePutFileChunk(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the file id in the URI.")
		}
		versionID, err := strconv.ParseInt(c.Param("versionID"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid string was not used for the version id in the URI.")
		}
		chunkNumber, err := strconv.ParseInt(c.Param("chunknumber"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the chunk number in the URI.")
		}
		chunkHash := c.Param("chunkhash")
		if chunkHash == "" {
			return c.String(http.StatusBadRequest, "A valid string was not used for the chunk hash.")
		}

		// get a byte limited reader, set to the maximum chunk size supported by Storage
		// plus a little extra space for cryptography information
		r := c.Request()
		w := c.Response().Writer
		bodyReader := http.MaxBytesReader(w, r.Body, state.Storage.ChunkSize+128)
		defer bodyReader.Close()
		chunk, err := ioutil.ReadAll(bodyReader)
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to read the chunk: "+err.Error())
		}

		// AddFileChunk does verify that the user ID owns the fild ID so we don't need
		// to replicate that work here, just add the chunk.
		fc, err := state.Storage.AddFileChunk(claims.UserID, int(fileID), int(versionID), int(chunkNumber), chunkHash, chunk)
		if err != nil || fc == nil {
			return c.String(http.StatusInternalServerError, "Failed to add the chunk to storage: "+err.Error())
		}

		return c.JSON(http.StatusOK, &models.FileChunkPutResponse{
			Status: true,
		})
	}
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFileChunks(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the file id in the URI.")
		}
		versionID, err := strconv.ParseInt(c.Param("versionID"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid string was not used for the version id in the URI.")
		}

		chunks, err := state.Storage.GetFileChunkInfos(claims.UserID, int(fileID), int(versionID))
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to get the chunk informations for the file id in the URI.")
		}

		return c.JSON(http.StatusOK, &models.FileChunksGetResponse{
			Chunks: chunks,
		})
	}
}

// handleGetFile returns a JSON object with all of the FileInfo data for the file in Storage
// as well as a slice of missing chunks, if any.
func handleGetFileChunk(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the file id in the URI.")
		}
		versionID, err := strconv.ParseInt(c.Param("versionID"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid string was not used for the version id in the URI.")
		}
		chunkNumber, err := strconv.ParseInt(c.Param("chunknumber"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the chunk number in the URI.")
		}

		// get the file info first to ensure ownership
		fi, err := state.Storage.GetFileInfo(claims.UserID, int(fileID))
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to get the file information for the file id in the URI.")
		}
		if fi.UserID != claims.UserID {
			return c.String(http.StatusForbidden, "Access denied.")
		}

		chunk, err := state.Storage.GetFileChunk(int(fileID), int(chunkNumber), int(versionID))
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to get the chunk information for the file id and chunk number in the URI.")
		}

		return c.Blob(http.StatusOK, "application/octet-stream", chunk.Chunk)
	}
}

// handlePutFile registers a file for a given user.
func handlePutFile(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// deserialize the JSON object that should be in the request body
		var req models.FilePutRequest
		err := c.Bind(&req)
		if err != nil {
			return c.String(http.StatusBadRequest, "Failed to read the request body: "+err.Error())
		}

		// sanity check some input
		if len(req.FileName) < 1 {
			return c.String(http.StatusBadRequest, "fileName must be supplied in the request")
		}
		if req.LastMod < 1 {
			return c.String(http.StatusBadRequest, "lastMod time must be supplied in the request")
		}
		if req.ChunkCount < 0 {
			return c.String(http.StatusBadRequest, "chunkCount must be supplied in the request")
		}
		if len(req.FileHash) < 1 && !req.IsDir {
			return c.String(http.StatusBadRequest, "fileHash must be supplied in the request")
		}

		// register a new file in storage with the information
		fi, err := state.Storage.AddFileInfo(claims.UserID, req.FileName, req.IsDir, req.Permissions, req.LastMod, req.ChunkCount, req.FileHash)
		if err != nil {
			return c.String(http.StatusConflict, "Failed to put a new file in storage for the user. "+err.Error())
		}

		return c.JSON(http.StatusOK, &models.FilePutResponse{
			FileInfo: *fi,
		})
	}
}

func handleDeleteFile(state *serverState) echo.HandlerFunc {
	return func(c echo.Context) error {
		jwtToken := c.Get(jwtContextName).(*jwt.Token)
		claims := jwtToken.Claims.(*jwtCustomClaims)

		// pull the file id from the URI matched by the mux
		fileID, err := strconv.ParseInt(c.Param("fileid"), 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "A valid integer was not used for the file id in the URI.")
		}

		// delete a file from storage with the information
		err = state.Storage.RemoveFile(claims.UserID, int(fileID))
		if err != nil {
			return c.String(http.StatusConflict, "Failed to remove a file in storage for the user. "+err.Error())
		}

		return c.JSON(http.StatusOK, &models.FileDeleteResponse{Success: true})
	}
}

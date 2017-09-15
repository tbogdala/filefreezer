// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package models

import "github.com/tbogdala/filefreezer"

// ServerCapabilities gets returned to the user to describe the features
// that the server has to the client.
type ServerCapabilities struct {
	ChunkSize int64
}

// UserLoginResponse is the JSON serializable response given by the
// /api/users/login POST handlder.
type UserLoginResponse struct {
	Token        string
	CryptoHash   []byte
	Capabilities ServerCapabilities
}

// UserCryptoHashUpdateRequest is the JSON serializable request sent to the
// /api/user/cryptohash PUT handler.
type UserCryptoHashUpdateRequest struct {
	CryptoHash []byte
}

// UserCryptoHashUpdateResponse is the JSON serializable response given by the
// /api/user/cryptohash PUT handler.
type UserCryptoHashUpdateResponse struct {
	Status bool
}

// UserStatsGetResponse is the JSON serializable response given by the
// /api/user/stats GET handler.
type UserStatsGetResponse struct {
	Stats filefreezer.UserStats
}

// AllFilesGetResponse is the JSON serializable response given by the
// /api/files GET handlder.
type AllFilesGetResponse struct {
	Files []filefreezer.FileInfo
}

// FileGetResponse is the JSON serializable response given by the
// /api/file/{id} GET handlder.
type FileGetResponse struct {
	filefreezer.FileInfo
	MissingChunks []int
}

// NewFileVersionRequest is the JSON serializable request object sent to the
// /api/file/{fileid}/version POST handler.
type NewFileVersionRequest struct {
	Permissions uint32
	LastMod     int64
	ChunkCount  int
	FileHash    string
}

// NewFileVersionResponse is the  JSON serializable response given by the
// /api/file/{fileid}/version POST handler.
type NewFileVersionResponse struct {
	filefreezer.FileInfo
	Status bool
}

// FileGetAllVersionsResponse is the  JSON serializable response given by the
// /api/file/{fileid}/versions GET handler.
type FileGetAllVersionsResponse struct {
	VersionIDs     []int
	VersionNumbers []int
}

// FileGetByNameRequest is the JSON structure to be sent to the
// /api/file/name GET handler.
type FileGetByNameRequest struct {
	FileName string
}

// FileChunkPutResponse is the JSON serializable response given by the
// /api/chunk/{id}/{versionID}/{chunknum} PUT handlder.
type FileChunkPutResponse struct {
	Status bool
}

// FileChunksGetResponse is the JSON serializable response given by the
// /api/chunk/{fileid}/{versionID}/ GET handlder.
type FileChunksGetResponse struct {
	Chunks []filefreezer.FileChunk
}

// FileChunkGetResponse is the JSON serializable response given by the
// /api/chunk/{fileid}/{versionID}/{chunknumber} GET handlder.
type FileChunkGetResponse struct {
	Chunk filefreezer.FileChunk
}

// FilePutResponse is the JSON serializable response given by the
// /api/files PUT handlder.
type FilePutResponse struct {
	filefreezer.FileInfo
}

// FilePutRequest is the JSON serializable request object sent to the
// /api/files PUT handlder.
type FilePutRequest struct {
	FileName    string
	IsDir       bool
	Permissions uint32
	LastMod     int64
	ChunkCount  int
	FileHash    string
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

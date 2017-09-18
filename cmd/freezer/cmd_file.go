// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// getFileInfoByFilename takes the long way of finding a FileInfo object
// by scanning all FileInfo objects registered for a given user. If a matching
// file is found it is returned and the error value will be null; otherwise
// an error will be set.
func (s *commandState) getFileInfoByFilename(filename string) (foundFile filefreezer.FileInfo, e error) {
	// get the entire file info list so that we can go through each file info
	// and find the right one for a given filename.
	// NOTE: implemented like this to support encrypted filenames.
	allFileInfos, err := s.getAllFileHashes()
	if err != nil {
		return foundFile, fmt.Errorf("failed to getall of the file hashes: %v", err)
	}

	// iterate through all of the files
	for _, fi := range allFileInfos {
		decryptedFilename, err := s.decryptString(fi.FileName)
		if err != nil {
			return foundFile, err
		}

		if decryptedFilename == filename {
			return fi, nil
		}
	}

	return foundFile, fmt.Errorf("could not find the file: %s", filename)
}

func (s *commandState) rmFile(filename string) error {
	fi, err := s.getFileInfoByFilename(filename)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/api/file/%d", s.hostURI, fi.FileID)
	_, err = runAuthRequest(target, "DELETE", s.authToken, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file %s: %v", filename, err)
	}

	logPrintf("Removed file: %s\n", filename)

	return nil
}

func (s *commandState) rmFileByID(fileID int) error {
	target := fmt.Sprintf("%s/api/file/%d", s.hostURI, fileID)
	_, err := runAuthRequest(target, "DELETE", s.authToken, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file by file ID (%d): %v", fileID, err)
	}

	logPrintf("Removed file by ID: %d\n", fileID)

	return nil
}

func (s *commandState) addFile(filename string, remoteFilepath string, isDir bool, permissions uint32, lastMod int64, chunkCount int, fileHash string) (fi filefreezer.FileInfo, err error) {
	// encrypt the remote filepath so that the server doesn't see the plaintext version
	cryptoRemoteName, err := s.encryptString(remoteFilepath)
	if err != nil {
		return fi, fmt.Errorf("Could not encrypt the remote file name before uploading: %v", err)
	}

	var putReq models.FilePutRequest
	putReq.FileName = cryptoRemoteName
	putReq.LastMod = lastMod
	putReq.ChunkCount = chunkCount
	putReq.FileHash = fileHash
	putReq.IsDir = isDir
	putReq.Permissions = permissions

	target := fmt.Sprintf("%s/api/files", s.hostURI)
	body, err := runAuthRequest(target, "POST", s.authToken, putReq)
	if err != nil {
		return fi, err
	}

	// if the POST fails or the response is bad, then the file wasn't registered
	// with the freezer, so there's nothing to rollback -- just return.
	var putResp models.FilePutResponse
	err = json.Unmarshal(body, &putResp)
	if err != nil {
		return fi, err
	}

	// we've registered the file, so now we should sync it
	_, _, err = s.syncFile(filename, remoteFilepath)

	return putResp.FileInfo, err
}

func (s *commandState) getFileVersions(filename string) (versionIDs []int, versionNums []int, err error) {
	fi, err := s.getFileInfoByFilename(filename)
	if err != nil {
		return nil, nil, err
	}

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/%d/versions", s.hostURI, fi.FileID)
	body, err := runAuthRequest(target, "GET", s.authToken, nil)
	var r models.FileGetAllVersionsResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to get the file versions: %v", err)
	}

	logPrintf("Registered versions for %s:\n", filename)
	logPrintln(strings.Repeat("=", 25+len(filename)))

	// loop through all of the results and print them
	for i, vID := range r.VersionIDs {
		logPrintf("Version ID: %d\t\tNumber: %d", vID, r.VersionNumbers[i])
	}

	return r.VersionIDs, r.VersionNumbers, nil
}

func (s *commandState) getMissingChunksForFile(fileID int) ([]int, error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/%d", s.hostURI, fileID)
	body, err := runAuthRequest(target, "GET", s.authToken, nil)
	var r models.FileGetResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the file's missing chunk list: %v", err)
	}

	return r.MissingChunks, nil
}

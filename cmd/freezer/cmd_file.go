// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

func (s *commandState) rmFile(filename string) error {
	var getReq models.FileGetByNameRequest
	getReq.FileName = filename

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/name", s.hostURI)
	body, err := runAuthRequest(target, "GET", s.authToken, getReq)
	var fi models.FileGetResponse
	err = json.Unmarshal(body, &fi)
	if err != nil {
		return fmt.Errorf("Failed to get the file information for the file name given (%s): %v", filename, err)
	}

	target = fmt.Sprintf("%s/api/file/%d", s.hostURI, fi.FileID)
	body, err = runAuthRequest(target, "DELETE", s.authToken, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file %s: %v", filename, err)
	}

	log.Printf("Removed file: %s\n", filename)

	return nil
}

func (s *commandState) addFile(fileName string, remoteFilepath string, isDir bool, permissions uint32, lastMod int64, chunkCount int, fileHash string) (fi filefreezer.FileInfo, err error) {
	var putReq models.FilePutRequest
	putReq.FileName = remoteFilepath
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
	_, _, err = s.syncFile(fileName, remoteFilepath)

	return putResp.FileInfo, err
}

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

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

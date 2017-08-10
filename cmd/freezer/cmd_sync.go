// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

const (
	syncStatusMissing     = 1
	syncStatusLocalNewer  = 2
	syncStatusRemoteNewer = 3
	syncStatusSame        = 4
)

func (s *commandState) syncDirectory(localDir string, remoteDir string) (changeCount int, e error) {
	changeCount = 0

	// make a map of filenames that have been processed locally so that the
	// loop that processes remote files can skip local files that have already
	// been sync'd.
	alreadyProccessed := make(map[string]bool)

	// get all of the remote files
	remoteFileHashes, err := s.getAllFileHashes()
	if err != nil {
		return 0, fmt.Errorf("Failed to a list of remote file hashes: %v", err)
	}
	var processDir func(localDir string, remoteDir string) (changeCount int, e error)
	processDir = func(localDir string, remoteDir string) (changeCount int, e error) {
		// silently return if the directory does not exist
		if _, err := os.Stat(localDir); os.IsNotExist(err) {
			return 0, nil
		}

		// get all of the local files
		localFileInfos, err := ioutil.ReadDir(localDir)
		if err != nil {
			return 0, fmt.Errorf("Failed to a list of local file names: %v", err)
		}

		// sync all of the local files
		var localFileInfo os.FileInfo
		for _, localFileInfo = range localFileInfos {
			localFileName := localDir + "/" + localFileInfo.Name()
			remoteFileName := remoteDir + "/" + localFileInfo.Name()

			// process directories by recursively looking into them for local files
			// and other directories.
			if localFileInfo.IsDir() {
				changes, err := processDir(localFileName, remoteFileName)
				if err != nil {
					return changes, err
				}
				changeCount += changes
				continue
			}

			// attempt the local file sync operation
			_, changes, err := s.syncFile(localFileName, remoteFileName)
			if err != nil {
				return changeCount, fmt.Errorf("Failed to sync local file (%s) with the remote file (%s): %v", localFileName, remoteFileName, err)
			}

			// on success, keep processing and update the change count
			changeCount += changes
			alreadyProccessed[localFileName] = true
		}

		return changeCount, nil
	}

	// start recursively processing at the local directory specified
	changeCount, e = processDir(localDir, remoteDir)
	if e != nil {
		return changeCount, e
	}

	// sync all of the remote files
	for _, remoteFileHash := range remoteFileHashes {
		remoteFileName := remoteFileHash.FileName

		// skip the remote file if we don't start with the right prefix
		if !strings.HasPrefix(remoteFileName, remoteDir) {
			continue
		}

		// build the local file path
		localFileName := localDir + remoteFileName[len(remoteDir):]

		// have we already processed it?
		_, processed := alreadyProccessed[localFileName]
		if processed {
			continue
		}

		dirIndex := strings.LastIndex(localFileName, "/")
		if dirIndex > 0 {
			// ensure the directory exists already
			// FIXME: DIRECTORY PERMISSIONS ARE NOT SAVED
			dirToCreate := localFileName[:dirIndex]
			err = os.MkdirAll(dirToCreate, 0777)
			if err != nil {
				return changeCount, fmt.Errorf("Failed to create the local directory for %s: %v", localDir, err)
			}
		}

		// attempt the remote file sync
		_, changes, err := s.syncFile(localFileName, remoteFileName)
		if err != nil {
			return changeCount, fmt.Errorf("Failed to sync remote file (%s) with the local file (%s): %v", remoteFileName, localFileName, err)
		}

		// on success, keep processing and update the change count
		changeCount += changes
	}

	return changeCount, nil
}

func (s *commandState) syncFile(localFilename string, remoteFilepath string) (status int, changeCount int, e error) {
	var getReq models.FileGetByNameRequest
	var remote models.FileGetResponse

	// get the file information for the filename, which provides
	// all of the information necessary to determine what to sync.
	getReq.FileName = remoteFilepath
	target := fmt.Sprintf("%s/api/file/name", s.hostURI)
	body, err := runAuthRequest(target, "GET", s.authToken, getReq)

	// if the file is not registered with the storage server, then upload it ...
	// futher checking will be unnecessary.
	if err != nil {
		localChunkCount, localLastMod, localPerms, localHash, err := filefreezer.CalcFileHashInfo(s.serverCapabilities.ChunkSize, localFilename)
		if err != nil {
			return syncStatusMissing, 0, fmt.Errorf("Failed to calculate the file hash data for %s: %v", localFilename, err)
		}
		ulCount, err := s.syncUpload(localFilename, remoteFilepath, false, localPerms, localLastMod, localChunkCount, localHash)
		if err != nil {
			return syncStatusMissing, ulCount, fmt.Errorf("Failed to upload the file to the server %s: %v", s.hostURI, err)
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
		dlCount, err := s.syncDownload(remote.FileID, remote.CurrentVersion.VersionID, localFilename,
			remoteFilepath, remote.CurrentVersion.ChunkCount)
		return syncStatusRemoteNewer, dlCount, err
	}

	// calculate some of the local file information
	localChunkCount, localLastMod, localPermissions, localHash, err := filefreezer.CalcFileHashInfo(s.serverCapabilities.ChunkSize, localFilename)
	if err != nil {
		return 0, 0, fmt.Errorf("Failed to calculate the file hash data for %s: %v", localFilename, err)
	}

	// lets prove that we don't need to do anything for some cases
	// NOTE: a lastMod difference here doesn't trigger a difference if other metrics check out the same
	// NOTE: a difference in permissions also doesn't trigger a difference
	if localHash == remote.CurrentVersion.FileHash && len(remote.MissingChunks) == 0 && localChunkCount == remote.CurrentVersion.ChunkCount {
		different := false
		if *flagExtraStrict {
			// now we get a chunk list for the file
			var remoteChunks models.FileChunksGetResponse
			target := fmt.Sprintf("%s/api/chunk/%d/%d", s.hostURI, remote.FileID, remote.CurrentVersion.VersionID)
			body, err := runAuthRequest(target, "GET", s.authToken, nil)
			err = json.Unmarshal(body, &remoteChunks)
			if err != nil {
				return 0, 0, fmt.Errorf("Failed to get the file chunk list for the file name given (%s): %v", remoteFilepath, err)
			}

			// sanity check
			remoteChunkCount := len(remoteChunks.Chunks)
			if localChunkCount == remoteChunkCount {
				// check the local chunks against remote hashes
				err = forEachChunk(int(s.serverCapabilities.ChunkSize), localFilename, localChunkCount, func(i int, b []byte) (bool, error) {
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
	if localLastMod > remote.CurrentVersion.LastMod {
		ulCount, e := s.syncUploadNewer(remote.FileID, localFilename, remoteFilepath, false, localPermissions, localLastMod, localChunkCount, localHash)
		return syncStatusLocalNewer, ulCount, e
	}

	if localLastMod < remote.CurrentVersion.LastMod {
		dlCount, e := s.syncDownload(remote.FileID, remote.CurrentVersion.VersionID, localFilename,
			remoteFilepath, remote.CurrentVersion.ChunkCount)
		return syncStatusRemoteNewer, dlCount, e
	}

	// there's been a difference detected in the files, but the mod times were the same, so
	// we attempt to upload any missing chunks.
	if len(remote.MissingChunks) > 0 {
		ulCount, e := s.syncUploadMissing(remote.FileID, remote.CurrentVersion.VersionID, localFilename, remoteFilepath, localChunkCount)
		return syncStatusMissing, ulCount, e
	}

	// we checked to make sure it was the same above, but we found it different -- however, no steps to
	// resolve this were taken, so through an error.
	return 0, 0, fmt.Errorf("found differences between local (%s) and remote (%s) versions, but this was not reconcilled", localFilename, remoteFilepath)
}

func (s *commandState) syncUploadMissing(remoteID int, remoteVersionID int, filename string, remoteFilepath string, localChunkCount int) (uploadCount int, e error) {
	// upload each chunk
	err := forEachChunk(int(s.serverCapabilities.ChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		target := fmt.Sprintf("%s/api/chunk/%d/%d/%d/%s", s.hostURI, remoteID, remoteVersionID, i, chunkHash)
		body, err := runAuthRequest(target, "PUT", s.authToken, b)
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

func (s *commandState) syncUploadNewer(remoteFileID int, filename string, remoteFilepath string, isDir bool, localPermissions uint32, localLastMod int64, localChunkCount int, localHash string) (uploadCount int, e error) {
	// make sure to delete the remote file
	target := fmt.Sprintf("%s/api/file/%d", s.hostURI, remoteFileID)
	_, err := runAuthRequest(target, "DELETE", s.authToken, nil)
	if err != nil {
		return 0, fmt.Errorf("Failed to remove the file %d: %v", remoteFileID, err)
	}
	log.Printf("%s XXX deleted remote", filename)

	// now upload the local file
	return s.syncUpload(filename, remoteFilepath, isDir, localPermissions, localLastMod, localChunkCount, localHash)
}

func (s *commandState) syncUpload(filename string, remoteFilepath string, isDir bool, localPermissions uint32, localLastMod int64, localChunkCount int, localHash string) (uploadCount int, e error) {
	// establish a new file on the remote freezer
	var putReq models.FilePutRequest
	putReq.FileName = remoteFilepath
	putReq.IsDir = isDir
	putReq.Permissions = localPermissions
	putReq.LastMod = localLastMod
	putReq.ChunkCount = localChunkCount
	putReq.FileHash = localHash
	target := fmt.Sprintf("%s/api/files", s.hostURI)
	body, err := runAuthRequest(target, "POST", s.authToken, putReq)
	if err != nil {
		return 0, err
	}

	var putResp models.FilePutResponse
	err = json.Unmarshal(body, &putResp)
	if err != nil {
		return 0, err
	}

	var getFileInfoResp models.FileGetResponse
	target = fmt.Sprintf("%s/api/file/%d", s.hostURI, putResp.FileID)
	body, err = runAuthRequest(target, "GET", s.authToken, nil)
	err = json.Unmarshal(body, &getFileInfoResp)
	if err != nil {
		return 0, err
	}

	remoteID := putResp.FileID
	remoteVersionID := getFileInfoResp.CurrentVersion.VersionID

	// upload each chunk
	err = forEachChunk(int(s.serverCapabilities.ChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		target = fmt.Sprintf("%s/api/chunk/%d/%d/%d/%s", s.hostURI, remoteID, remoteVersionID, i, chunkHash)
		body, err = runAuthRequest(target, "PUT", s.authToken, b)
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

func (s *commandState) syncDownload(remoteID int, remoteVersionID int, filename string, remoteFilepath string, chunkCount int) (downloadCount int, e error) {
	localFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return 0, fmt.Errorf("Failed to open local file (%s) for writing: %v", filename, err)
	}
	defer localFile.Close()

	// download each chunk and write it out to the file
	chunksWritten := 0
	for i := 0; i < chunkCount; i++ {
		target := fmt.Sprintf("%s/api/chunk/%d/%d/%d", s.hostURI, remoteID, remoteVersionID, i)
		body, err := runAuthRequest(target, "GET", s.authToken, nil)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to get the file chunk #%d for file id%d: %v", i, remoteID, err)
		}

		var chunkResp models.FileChunkGetResponse
		err = json.Unmarshal(body, &chunkResp)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to get the file chunk #%d for file id%d: %v", i, remoteID, err)
		}

		// write out the chunk that was downloaded
		chunk := chunkResp.Chunk.Chunk
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

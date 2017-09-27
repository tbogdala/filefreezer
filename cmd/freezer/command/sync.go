// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package command

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// SyncStatus enumeration used to indicate the findings of the SyncFile and
// SyncDirectory functions.
const (
	SyncStatusMissing             = 1 // chunks missing
	SyncStatusLocalNewer          = 2 // local file newer
	SyncStatusRemoteNewer         = 3 // remote file newer
	SyncStatusSame                = 4 // local and remote files are the same
	SyncStatusUnsupportedFileType = 5 // returned when sync encouters device files or socket files, etc...
)

const (
	// SyncCurrentVersion is the value to pass to SyncFile to sync the current version
	// of the file and not a particular version number.
	SyncCurrentVersion = 0
)

// SyncDirectory will take a localDir and recursively walk the filesystem calling SyncFile
// for each file encountered. remoteDir can be specified to prefix the remote filepath
// for each file. The total number of changed chunks is returned and upon error a non-nil
// error value is returned.
func (s *State) SyncDirectory(localDir string, remoteDir string) (changeCount int, e error) {
	changeCount = 0

	// make a map of filenames that have been processed locally so that the
	// loop that processes remote files can skip local files that have already
	// been sync'd.
	alreadyProccessed := make(map[string]bool)

	// get all of the remote files
	remoteFileHashes, err := s.GetAllFileHashes()
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
			return 0, fmt.Errorf("Failed to get a list of local file names: %v", err)
		}

		// sync all of the local files
		var localFileInfo os.FileInfo
		for _, localFileInfo = range localFileInfos {
			localFileName := localDir + "/" + localFileInfo.Name()
			remoteFileName := remoteDir + "/" + localFileInfo.Name()

			// process directories by recursively looking into them for local files
			// and other directories; after that, add the directory itself
			if localFileInfo.IsDir() {
				changes, err := processDir(localFileName, remoteFileName)
				if err != nil {
					return changes, err
				}
				changeCount += changes
			}

			// attempt the local file sync operation
			_, changes, err := s.SyncFile(localFileName, remoteFileName, SyncCurrentVersion)
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
		remoteFileName, err := s.DecryptString(remoteFileHash.FileName)
		if err != nil {
			return 0, fmt.Errorf("Failed to decrypt remote file name for file id %d: %v", remoteFileHash.FileID, err)
		}

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
		_, changes, err := s.SyncFile(localFileName, remoteFileName, SyncCurrentVersion)
		if err != nil {
			return changeCount, fmt.Errorf("Failed to sync remote file (%s) with the local file (%s): %v", remoteFileName, localFileName, err)
		}

		// on success, keep processing and update the change count
		changeCount += changes
	}

	return changeCount, nil
}

// SyncFile will synchronize the localFilename which is identified as remoteFilepath on the server.
// A versionNum can also be specified (or left at <=0 for current version) to pick a particular version to sync.
// A sync status enumeration value is returned indicating if chunks were missing or whether or not
// the local or remote version were considered newer. The number of chunks changes is also returned and
// a non-nil error value is returned on error.
func (s *State) SyncFile(localFilename string, remoteFilepath string, versionNum int) (status int, changeCount int, e error) {
	// make sure that we're not attempting to sync a symlink, device, named pipe or socket
	localFileStat, localFileStatErr := os.Stat(localFilename)
	if localFileStatErr == nil {
		// only check local files that exist
		localMode := localFileStat.Mode()
		if (localMode&os.ModeCharDevice) != 0 ||
			(localMode&os.ModeDevice) != 0 ||
			(localMode&os.ModeNamedPipe) != 0 ||
			(localMode&os.ModeSocket) != 0 ||
			(localMode&os.ModeSymlink) != 0 {
			return SyncStatusUnsupportedFileType, 0, nil
		}
	}

	// get the file information for the filename, which provides
	// all of the information necessary to determine what to sync.
	remote, err := s.GetFileInfoByFilename(remoteFilepath)

	// if the file is not registered with the storage server, then upload it ...
	// futher checking will be unnecessary.
	if err != nil {
		localStats, err := filefreezer.CalcFileHashInfo(s.ServerCapabilities.ChunkSize, localFilename)
		if err != nil {
			return SyncStatusMissing, 0, fmt.Errorf("Failed to calculate the file hash data for file %s to upload as %s: %v", localFilename, remoteFilepath, err)
		}
		ulCount, err := s.syncUploadNew(localFilename, remoteFilepath, localStats.IsDir,
			localStats.Permissions, localStats.LastMod, localStats.ChunkCount, localStats.HashString)
		if err != nil {
			return SyncStatusMissing, ulCount, fmt.Errorf("Failed to upload the file to the server %s: %v", s.HostURI, err)
		}
		return SyncStatusLocalNewer, ulCount, nil
	}

	// we got a valid response so the file is registered on the server;
	// pull all of the versions for this file so that we can target the
	// correct VersionID for a given versionNum.
	var syncVersion *filefreezer.FileVersionInfo
	if versionNum != SyncCurrentVersion {
		versions, err := s.GetFileVersions(remoteFilepath)
		if err != nil {
			return 0, 0, fmt.Errorf("Couldn't get all of the file version for %s: %v", remoteFilepath, err)
		}
		for _, v := range versions {
			if v.VersionNumber == versionNum {
				syncVersion = &v
				break
			}
		}
	}

	// if we were not looking for the current version or the version number
	// specified was not found in the list of file versions, then set the
	// versionID to sync against to the current version ID for the file.
	if syncVersion == nil {
		syncVersion = &remote.CurrentVersion
	}

	if os.IsNotExist(localFileStatErr) {
		// if it is a local file that doesn't exist then download the file from the
		// server if it is registered there.
		if !remote.IsDir {
			dlCount, err := s.syncDownload(remote.FileID, syncVersion.VersionID, localFilename,
				remoteFilepath, syncVersion.ChunkCount)
			return SyncStatusRemoteNewer, dlCount, err
		}

		// if its a local directory that doesn't exist, then just create the directory
		err = os.MkdirAll(localFilename, os.ModeDir|os.FileMode(syncVersion.Permissions))
		if err != nil {
			return SyncStatusRemoteNewer, 0, err
		}

		s.Printf("%s <== directory created", remoteFilepath)
		return SyncStatusRemoteNewer, 0, nil
	}

	// At this point the it is registered on the server and the local file exists,
	// so it is time to calculate hash information and do comparisons ...

	// calculate some of the local file information
	localStats, err := filefreezer.CalcFileHashInfo(s.ServerCapabilities.ChunkSize, localFilename)
	if err != nil {
		return 0, 0, fmt.Errorf("Failed to calculate the local file hash data for %s: %v", localFilename, err)
	}

	// if this is a directory we're syncing, the above scenarios cover registering
	// it with the server and creating it locally.
	// NOTE: at this point, lastmod and permissions are not synced between
	// remote and local filesystems because there's no way to determine
	// which is authoritative.
	if localStats.IsDir {
		return SyncStatusSame, 0, nil
	}

	// handle a special case here for when a particular version is requested that
	// is not the current version. in this case we will compare file hashes and
	// download the remote version of the file if the hashes are not equal
	if syncVersion.VersionID != remote.CurrentVersion.VersionID {
		if localStats.HashString != syncVersion.FileHash {
			dlCount, err := s.syncDownload(remote.FileID, syncVersion.VersionID, localFilename,
				remoteFilepath, syncVersion.ChunkCount)
			return SyncStatusRemoteNewer, dlCount, err
		}
	}

	// pull the list of missing chunks for the file
	remoteMissingChunks, err := s.GetMissingChunksForFile(remote.FileID)
	if err != nil {
		return SyncStatusSame, 0, err
	}

	// lets prove that we don't need to do anything for some cases
	// NOTE: a lastMod difference here doesn't trigger a difference if other metrics check out the same
	// NOTE: a difference in permissions also doesn't trigger a difference
	if localStats.HashString == remote.CurrentVersion.FileHash &&
		len(remoteMissingChunks) == 0 &&
		localStats.ChunkCount == remote.CurrentVersion.ChunkCount {
		different := false
		if s.ExtraStrict {
			// now we get a chunk list for the file
			var remoteChunks models.FileChunksGetResponse
			target := fmt.Sprintf("%s/api/chunk/%d/%d", s.HostURI, remote.FileID, remote.CurrentVersion.VersionID)
			body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
			err = json.Unmarshal(body, &remoteChunks)
			if err != nil {
				return 0, 0, fmt.Errorf("Failed to get the file chunk list for the file name given (%s): %v", remoteFilepath, err)
			}

			// sanity check
			remoteChunkCount := len(remoteChunks.Chunks)
			if localStats.ChunkCount == remoteChunkCount {
				// check the local chunks against remote hashes
				err = forEachChunk(int(s.ServerCapabilities.ChunkSize), localFilename, localStats.ChunkCount, func(i int, b []byte) (bool, error) {
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
			s.Printf("%s --- unchanged", remoteFilepath)
			return SyncStatusSame, 0, nil
		}
	}

	// at this point we have a file difference. we'll use the local file as the source of truth
	// if it's lastMod is newer than the remote file.
	if localStats.LastMod > remote.CurrentVersion.LastMod {
		ulCount, e := s.syncUploadNewer(remote.FileID, localFilename, remoteFilepath, localStats.IsDir,
			localStats.Permissions, localStats.LastMod, localStats.ChunkCount, localStats.HashString)
		return SyncStatusLocalNewer, ulCount, e
	}

	if localStats.LastMod < remote.CurrentVersion.LastMod {
		dlCount, e := s.syncDownload(remote.FileID, remote.CurrentVersion.VersionID, localFilename,
			remoteFilepath, remote.CurrentVersion.ChunkCount)
		return SyncStatusRemoteNewer, dlCount, e
	}

	// there's been a difference detected in the files, but the mod times were the same, so
	// we attempt to upload any missing chunks.
	if len(remoteMissingChunks) > 0 {
		ulCount, e := s.syncUploadMissing(remote.FileID, remote.CurrentVersion.VersionID, localFilename, remoteFilepath, localStats.ChunkCount)
		return SyncStatusMissing, ulCount, e
	}

	// if we've got this far, we have a local and remote file with the same lastmod
	// but differing hashes. for this case we'll upload the local file as a newer version.
	if localStats.HashString != remote.CurrentVersion.FileHash &&
		localStats.LastMod == remote.CurrentVersion.LastMod {
		ulCount, e := s.syncUploadNewer(remote.FileID, localFilename, remoteFilepath, localStats.IsDir,
			localStats.Permissions, localStats.LastMod, localStats.ChunkCount, localStats.HashString)
		return SyncStatusLocalNewer, ulCount, e
	}

	// we checked to make sure it was the same above, but we found it different -- however, no steps to
	// resolve this were taken, so through an error.
	return 0, 0, fmt.Errorf("found differences between local (%s) and remote (%s) versions, "+
		"but this was not reconcilled; lastmod equality (%v); hash equality (%v)",
		localFilename, remoteFilepath,
		localStats.LastMod == remote.CurrentVersion.LastMod,
		localStats.HashString == remote.CurrentVersion.FileHash)
}

func (s *State) syncUploadMissing(remoteID int, remoteVersionID int, filename string, remoteFilepath string, localChunkCount int) (uploadCount int, e error) {
	// upload each chunk
	err := forEachChunk(int(s.ServerCapabilities.ChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk with unencrypted data
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		cryptoBytes, err := s.encryptBytes(b)
		if err != nil {
			return false, fmt.Errorf("Failed to encrypt chunk before sending to the server: %v", err)
		}

		target := fmt.Sprintf("%s/api/chunk/%d/%d/%d/%s", s.HostURI, remoteID, remoteVersionID, i, chunkHash)
		body, err := s.RunAuthRequest(target, "PUT", s.AuthToken, cryptoBytes)
		if err != nil {
			return false, err
		}

		var resp models.FileChunkPutResponse
		err = json.Unmarshal(body, &resp)
		if err != nil || resp.Status == false {
			return false, fmt.Errorf("Failed to upload the chunk to the server: %v", err)
		}

		s.Printf("%s +++ %d / %d", remoteFilepath, i+1, localChunkCount)
		uploadCount++

		return true, nil
	})
	if err != nil {
		return uploadCount, fmt.Errorf("Failed to upload the local file chunk for %s: %v", filename, err)
	}

	return uploadCount, nil
}

func (s *State) syncUploadNewer(remoteFileID int, filename string, remoteFilepath string, isDir bool, localPermissions uint32, localLastMod int64, localChunkCount int, localHash string) (uploadCount int, e error) {
	// tag a new version for the file
	var postReq models.NewFileVersionRequest
	postReq.LastMod = localLastMod
	postReq.Permissions = localPermissions
	postReq.ChunkCount = localChunkCount
	postReq.FileHash = localHash
	target := fmt.Sprintf("%s/api/file/%d/version", s.HostURI, remoteFileID)
	body, err := s.RunAuthRequest(target, "POST", s.AuthToken, postReq)
	if err != nil {
		return 0, fmt.Errorf("Failed to tag a new version for the file %d: %v", remoteFileID, err)
	}

	var postResp models.NewFileVersionResponse
	err = json.Unmarshal(body, &postResp)
	if err != nil {
		return 0, fmt.Errorf("Failed to read the response for tagging a new version for the file %d: %v", remoteFileID, err)
	}

	// if we're uploading a newer version for a directory we can just
	// stop here because there are no chunks to send.
	if isDir {
		return
	}

	fi := &postResp.FileInfo

	// upload each chunk
	err = forEachChunk(int(s.ServerCapabilities.ChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		cryptoBytes, err := s.encryptBytes(b)
		if err != nil {
			return false, fmt.Errorf("Failed to encrypt chunk before sending to the server: %v", err)
		}

		target = fmt.Sprintf("%s/api/chunk/%d/%d/%d/%s", s.HostURI, fi.FileID, fi.CurrentVersion.VersionID, i, chunkHash)
		body, err = s.RunAuthRequest(target, "PUT", s.AuthToken, cryptoBytes)
		if err != nil {
			return false, err
		}

		var resp models.FileChunkPutResponse
		err = json.Unmarshal(body, &resp)
		if err != nil || resp.Status == false {
			return false, fmt.Errorf("Failed to upload the chunk to the server: %v", err)
		}

		s.Printf("%s >>> %d / %d", remoteFilepath, i+1, localChunkCount)
		uploadCount++

		return true, nil
	})

	if err != nil {
		return uploadCount, fmt.Errorf("Failed to upload the local file chunk for %s: %v", filename, err)
	}

	return uploadCount, nil
}

func (s *State) syncUploadNew(filename string, remoteFilepath string, isDir bool, localPermissions uint32, localLastMod int64, localChunkCount int, localHash string) (uploadCount int, e error) {
	// encrypt the remote filepath so that the server doesn't see the plaintext version
	cryptoRemoteName, err := s.EncryptString(remoteFilepath)
	if err != nil {
		return 0, fmt.Errorf("Could not encrypt the remote file name before uploading: %v", err)
	}

	// establish a new file on the remote freezer
	var putReq models.FilePutRequest
	putReq.FileName = cryptoRemoteName
	putReq.IsDir = isDir
	putReq.Permissions = localPermissions
	putReq.LastMod = localLastMod
	putReq.ChunkCount = localChunkCount
	putReq.FileHash = localHash
	target := fmt.Sprintf("%s/api/files", s.HostURI)
	body, err := s.RunAuthRequest(target, "POST", s.AuthToken, putReq)
	if err != nil {
		return 0, err
	}

	var putResp models.FilePutResponse
	err = json.Unmarshal(body, &putResp)
	if err != nil {
		return 0, err
	}

	// if we're uploading a new directory, stop here because there are no
	// chunks to sync.
	if isDir == true {
		s.Printf("%s ==> directory created", remoteFilepath)
		return 0, nil
	}

	var getFileInfoResp models.FileGetResponse
	target = fmt.Sprintf("%s/api/file/%d", s.HostURI, putResp.FileID)
	body, err = s.RunAuthRequest(target, "GET", s.AuthToken, nil)
	err = json.Unmarshal(body, &getFileInfoResp)
	if err != nil {
		return 0, err
	}

	remoteID := putResp.FileID
	remoteVersionID := getFileInfoResp.CurrentVersion.VersionID

	// upload each chunk
	err = forEachChunk(int(s.ServerCapabilities.ChunkSize), filename, localChunkCount, func(i int, b []byte) (bool, error) {
		// hash the chunk
		hasher := sha1.New()
		hasher.Write(b)
		hash := hasher.Sum(nil)
		chunkHash := base64.URLEncoding.EncodeToString(hash)

		cryptoBytes, err := s.encryptBytes(b)
		if err != nil {
			return false, fmt.Errorf("Failed to encrypt chunk before sending to the server: %v", err)
		}

		target = fmt.Sprintf("%s/api/chunk/%d/%d/%d/%s", s.HostURI, remoteID, remoteVersionID, i, chunkHash)
		body, err = s.RunAuthRequest(target, "PUT", s.AuthToken, cryptoBytes)
		if err != nil {
			return false, err
		}

		var resp models.FileChunkPutResponse
		err = json.Unmarshal(body, &resp)
		if err != nil || resp.Status == false {
			return false, fmt.Errorf("Failed to upload the chunk to the server: %v", err)
		}

		s.Printf("%s >>> %d / %d", remoteFilepath, i+1, localChunkCount)
		uploadCount++

		return true, nil
	})
	if err != nil {
		return uploadCount, fmt.Errorf("Failed to upload the local file chunk for %s: %v", filename, err)
	}

	s.Printf("%s ==> uploaded", remoteFilepath)
	return uploadCount, nil
}

func (s *State) syncDownload(remoteID int, remoteVersionID int, filename string, remoteFilepath string, chunkCount int) (downloadCount int, e error) {
	localFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return 0, fmt.Errorf("Failed to open local file (%s) for writing: %v", filename, err)
	}
	defer localFile.Close()

	// download each chunk and write it out to the file
	chunksWritten := 0
	for i := 0; i < chunkCount; i++ {
		target := fmt.Sprintf("%s/api/chunk/%d/%d/%d", s.HostURI, remoteID, remoteVersionID, i)
		body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to get the file chunk #%d for file id%d: %v", i, remoteID, err)
		}

		// write out the chunk that was downloaded
		chunk := body
		uncryptoBytes, err := s.decryptBytes(chunk)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to decrypt the the chunk bytes: %v", err)
		}

		_, err = localFile.Write(uncryptoBytes)
		if err != nil {
			return chunksWritten, fmt.Errorf("Failed to write to the #%d chunk to the local file %s: %v", i, filename, err)
		}

		s.Printf("%s <<< %d / %d", remoteFilepath, i+1, chunkCount)
		chunksWritten++
	}

	s.Printf("%s <== downloaded", remoteFilepath)
	return chunksWritten, nil
}

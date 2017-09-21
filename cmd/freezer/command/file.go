// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package command

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/models"
)

// GetFileInfoByFilename takes the long way of finding a FileInfo object
// by scanning all FileInfo objects registered for a given user. If a matching
// file is found it is returned and the error value will be null; otherwise
// an error will be set.
// NOTE: implemented like this to support encrypted filenames.
func (s *State) GetFileInfoByFilename(filename string) (foundFile filefreezer.FileInfo, e error) {
	// get the entire file info list so that we can go through each file info
	// and find the right one for a given filename.
	allFileInfos, err := s.GetAllFileHashes()
	if err != nil {
		return foundFile, fmt.Errorf("failed to getall of the file hashes: %v", err)
	}

	// iterate through all of the files
	for _, fi := range allFileInfos {
		decryptedFilename, err := s.DecryptString(fi.FileName)
		if err != nil {
			return foundFile, err
		}

		if decryptedFilename == filename {
			return fi, nil
		}
	}

	return foundFile, fmt.Errorf("could not find the file: %s", filename)
}

// RmFile takes the filename and attempts to find it in the list of filenames
// registered on the storage server for the user. If it does find it, an
// API method is called to delete the object. A non-nil error is returned on failure.
func (s *State) RmFile(filename string) error {
	fi, err := s.GetFileInfoByFilename(filename)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fi.FileID)
	_, err = s.RunAuthRequest(target, "DELETE", s.AuthToken, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file %s: %v", filename, err)
	}

	s.Printf("Removed file: %s\n", filename)

	return nil
}

// RmFileByID takes the file id directly and an API method is called to
// delete the object. A non-nil error is returned on failure.
func (s *State) RmFileByID(fileID int) error {
	target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fileID)
	_, err := s.RunAuthRequest(target, "DELETE", s.AuthToken, nil)
	if err != nil {
		return fmt.Errorf("Failed to remove the file by file ID (%d): %v", fileID, err)
	}

	s.Printf("Removed file by ID: %d\n", fileID)

	return nil
}

// GetFileVersions will return a slice of global version IDs and a matching
// slice of version numbers for the filename provided. A non-nil error is returned on error.
func (s *State) GetFileVersions(filename string) (versionIDs []int, versionNums []int, err error) {
	fi, err := s.GetFileInfoByFilename(filename)
	if err != nil {
		return nil, nil, err
	}

	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/%d/versions", s.HostURI, fi.FileID)
	body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
	var r models.FileGetAllVersionsResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to get the file versions: %v", err)
	}

	s.Printf("Registered versions for %s:\n", filename)
	s.Println(strings.Repeat("=", 25+len(filename)))

	// loop through all of the results and print them
	for i, vID := range r.VersionIDs {
		s.Printf("Version ID: %d\t\tNumber: %d", vID, r.VersionNumbers[i])
	}

	return r.VersionIDs, r.VersionNumbers, nil
}

// GetMissingChunksForFile will return a slice of chunk numbers (index starts at zero and
// is local to the specific file) for a given file located by file ID. A non-nil
// error is returned on error.
func (s *State) GetMissingChunksForFile(fileID int) ([]int, error) {
	// get the file id for the filename provided
	target := fmt.Sprintf("%s/api/file/%d", s.HostURI, fileID)
	body, err := s.RunAuthRequest(target, "GET", s.AuthToken, nil)
	var r models.FileGetResponse
	err = json.Unmarshal(body, &r)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the file's missing chunk list: %v", err)
	}

	return r.MissingChunks, nil
}

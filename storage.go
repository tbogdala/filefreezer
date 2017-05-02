// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package filefreezer

import (
	"database/sql"
	"fmt"
	"sort"

	// import the sqlite3 driver for use with database/sql
	_ "github.com/mattn/go-sqlite3"
)

const (
	createUsersTable = `CREATE TABLE Users (
		UserID 		INTEGER PRIMARY KEY	NOT NULL,
		Name		TEXT	UNIQUE		ON CONFLICT ABORT,
		Salt		TEXT				NOT NULL,
		Password	BLOB				NOT NULL
	);`

	createPermsTable = `CREATE TABLE Perms (
		UserID 		INTEGER PRIMARY KEY	NOT NULL,
		Quota		INTEGER				NOT NULL
	);`

	createUserInfoTable = `CREATE TABLE UserInfo (
		UserID 		INTEGER PRIMARY KEY	NOT NULL,
		Allocated	INTEGER				NOT NULL,
		Revision	INTEGER				NOT NULL
	);`

	createFileInfoTable = `CREATE TABLE FileInfo (
		FileID 		INTEGER PRIMARY KEY	NOT NULL,
		UserID 		INTEGER 			NOT NULL,
		FileName	TEXT				NOT NULL,
		LastMod		INTEGER				NOT NULL,
		ChunkCount  INTEGER				NOT NULL,
		FileHash	TEXT				NOT NULL
	);`

	createFileChunksTable = `CREATE TABLE FileChunks (
		FileID 		INTEGER PRIMARY KEY	NOT NULL,
		ChunkNum	INTEGER 			NOT NULL,
		ChunkHash	TEXT				NOT NULL,
		Chunk		BLOB				NOT NULL
	);`

	lookupUserByName = `SELECT Name FROM Users WHERE Name = ?;`
	addUser          = `INSERT INTO Users (Name, Salt, Password) VALUES (?, ?, ?);`
	getUser          = `SELECT UserID, Salt, Password FROM Users  WHERE Name = ?;`
	setUserQuota     = `INSERT OR REPLACE INTO Perms (UserID, Quota) VALUES (?, ?);`
	getUserQuota     = `SELECT Quota FROM Perms WHERE UserID = ?;`
	setUserInfo      = `INSERT OR REPLACE INTO UserInfo (UserID, Allocated, Revision) VALUES (?, ?, ?);`
	getUserInfo      = `SELECT Allocated, Revision FROM UserInfo WHERE UserID = ?;`
	updateUserInfo   = `UPDATE UserInfo SET Allocated = Allocated + (?), Revision = Revision + 1 WHERE UserID = ?;`
	addFileInfo      = `INSERT INTO FileInfo (UserID, FileName, LastMod, ChunkCount, FileHash) SELECT ?, ?, ?, ?, ?
						  WHERE NOT EXISTS (SELECT 1 FROM FileInfo WHERE UserID = ? AND FileName = ?);`
	getFileInfo          = `SELECT UserID, FileName, LastMod, ChunkCount, FileHash FROM FileInfo WHERE FileID = ?;`
	getAllUserFiles      = `SELECT FileID, FileName, LastMod, ChunkCount, FileHash FROM FileInfo WHERE UserID = ?;`
	getAllFileChunksByID = `SELECT ChunkNum, ChunkHash FROM FileChunks WHERE FileID = ?;`
	addFileChunk         = `INSERT OR REPLACE INTO FileChunks (FileID, ChunkNum, ChunkHash, Chunk) 
							  VALUES (?, ?, ?, ?);`
	removeFileChunk = `DELETE FROM FileChunks WHERE FileID = ? AND ChunkNum = ?;`
	getFileChunk    = `SELECT ChunkHash, Chunk FROM FileChunks WHERE FileID = ? AND ChunkNum = ?;`
)

// Storage is the backend data model for the file storage logic.
type Storage struct {
	// ChunkSize is the number of bytes the chunk can maximally be
	ChunkSize int64

	// db is the database connection
	db *sql.DB
}

// NewStorage creates a new Storage object using the sqlite3
// driver at the path given.
func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("could not open the database (%s): %v", dbPath, err)
	}

	// make sure we can hit the database by pinging it; this
	// will detect potential connection problems early.
	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("could not ping the open database (%s): %v", dbPath, err)
	}

	s := new(Storage)
	s.db = db
	s.ChunkSize = 1024 * 1024 * 4 // 4MB
	return s, nil
}

// Close releases the backend connections to the database.
func (s *Storage) Close() {
	s.db.Close()
}

// CreateTables will create the tables needed in the database if they
// don't already exist. If the tables already exist an error will be returned.
func (s *Storage) CreateTables() error {
	_, err := s.db.Exec(createUsersTable)
	if err != nil {
		return fmt.Errorf("failed to create the USERS table: %v", err)
	}

	_, err = s.db.Exec(createPermsTable)
	if err != nil {
		return fmt.Errorf("failed to create the PERMS table: %v", err)
	}

	_, err = s.db.Exec(createUserInfoTable)
	if err != nil {
		return fmt.Errorf("failed to create the USERINFO table: %v", err)
	}

	_, err = s.db.Exec(createFileInfoTable)
	if err != nil {
		return fmt.Errorf("failed to create the FILEINFO table: %v", err)
	}

	_, err = s.db.Exec(createFileChunksTable)
	if err != nil {
		return fmt.Errorf("failed to create the FILECHUNKS table: %v", err)
	}

	return nil
}

// IsUsernameFree will return true if there is not already a username with the
// same text in the Users table.
func (s *Storage) IsUsernameFree(username string) (bool, error) {
	// attempt to see if the username is already taken
	rows, err := s.db.Query(lookupUserByName, username)
	if err != nil {
		return false, fmt.Errorf("failed to search the Users table for a username: %v", err)
	}
	defer rows.Close()

	// did we find it?
	var existingName string
	for rows.Next() {
		err := rows.Scan(&existingName)
		if err != nil {
			return false, fmt.Errorf("failed to scan the next row while searching for existing usernames: %v", err)
		}
		if existingName == username {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("failed to scan all of the search results for a username: %v", err)
	}

	return true, nil
}

// AddUser should create the user in the USERS table. The username should be unique.
// saltedHash should be the combined password & salt hash and salt should be
// the user specific generated salt.
//
// This function returns a true bool value if a user was created and false if
// the user was not created (e.g. username was already taken).
func (s *Storage) AddUser(username string, salt string, saltedHash []byte) (bool, error) {
	// insert the user into the table ... username uniqueness is enforced
	// as a sql ON CONFLICT ABORT which will fail the INSERT and return an err here.
	_, err := s.db.Exec(addUser, username, salt, saltedHash)
	if err != nil {
		return false, fmt.Errorf("failed to insert the new user (%s): %v", username, err)
	}

	return true, nil
}

// GetUser queries the Users table for a given username and returns the associated data.
// If the query fails and error will be returned.
func (s *Storage) GetUser(username string) (id int, salt string, saltedHash []byte, e error) {
	err := s.db.QueryRow(getUser, username).Scan(&id, &salt, &saltedHash)
	if err != nil {
		e = fmt.Errorf("failed to get the user information from the database: %v", err)
		return
	}

	e = nil
	return
}

// SetUserQuota sets the user quota for a user by user id.
// NOTE: This does not authenticate a user when setting the values!
func (s *Storage) SetUserQuota(userID int, quota int) error {
	_, err := s.db.Exec(setUserQuota, userID, quota)
	if err != nil {
		return fmt.Errorf("failed to set the user quota in the database: %v", err)
	}

	return nil
}

// GetUserQuota returns the user quota for a user by user id.
func (s *Storage) GetUserQuota(userID int) (quota int, e error) {
	err := s.db.QueryRow(getUserQuota, userID).Scan(&quota)
	if err != nil {
		e = fmt.Errorf("failed to get the user quota from the database: %v", err)
		return
	}

	e = nil
	return
}

// SetUserInfo sets the user information for a user by user id.
// NOTE: This does not authenticate a user when setting the values!
func (s *Storage) SetUserInfo(userID int, allocated int, revision int) error {
	_, err := s.db.Exec(setUserInfo, userID, allocated, revision)
	if err != nil {
		return fmt.Errorf("failed to set the user info in the database: %v", err)
	}

	return nil
}

// UpdateUserInfo increments the user's revision by one and updates the allocated
// byte counter with the new delta.
func (s *Storage) UpdateUserInfo(userID int, allocDelta int) error {
	res, err := s.db.Exec(updateUserInfo, allocDelta, userID)
	if err != nil {
		return fmt.Errorf("failed to update the user info in the database: %v", err)
	}

	// make sure one row was affected with the UPDATE statement
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to update the user info in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to update the user info in the database: %v", err)
	}

	return nil
}

// GetUserInfo returns the user information for a user by user id.
func (s *Storage) GetUserInfo(userID int) (allocated int, revision int, e error) {
	err := s.db.QueryRow(getUserInfo, userID).Scan(&allocated, &revision)
	if err != nil {
		e = fmt.Errorf("failed to get the user info from the database: %v", err)
		return
	}

	e = nil
	return
}

// AddFileInfo registers a new file for a given user which is identified by the filename string.
// lastmod (time in seconds since 1/1/1970) and the filehash string are provided as well. The
// chunkCount parameter should be the number of chunks required for the size of the file. If the
// file could not be added an error is returned, otherwise nil on success.
func (s *Storage) AddFileInfo(userID int, filename string, lastMod int64, chunkCount int, fileHash string) error {
	res, err := s.db.Exec(addFileInfo, userID, filename, lastMod, chunkCount, fileHash, userID, filename)
	if err != nil {
		return fmt.Errorf("failed to add a new file info in the database: %v", err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to add a new file info in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to add a new file info in the database: %v", err)
	}

	return nil
}

// UserFileInfo contains the information stored about a given file for a particular user.
type UserFileInfo struct {
	UserID     int
	FileID     int
	FileName   string
	LastMod    int64
	ChunkCount int
	FileHash   string
}

// GetAllUserFileInfos returns a slice of UserFileInfo objects that describe all known
// files in storage for a given user ID. If this query was unsuccessful and error is returned.
func (s *Storage) GetAllUserFileInfos(userID int) ([]UserFileInfo, error) {
	rows, err := s.db.Query(getAllUserFiles, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all of the file infos from the database: %v", err)
	}
	defer rows.Close()

	// iterate over the returned rows to create a new slice of file info objects
	result := []UserFileInfo{}
	for rows.Next() {
		var fi UserFileInfo
		err := rows.Scan(&fi.FileID, &fi.FileName, &fi.LastMod, &fi.ChunkCount, &fi.FileHash)
		if err != nil {
			return nil, fmt.Errorf("failed to scan the next row while processing user file infos: %v", err)
		}
		fi.UserID = userID
		result = append(result, fi)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan all of the search results for a user's file infos: %v", err)
	}

	return result, nil
}

// GetFileInfo returns a UserFileInfo object that describes the file identified
// by the fileID parameter. If this query was unsuccessful an error is returned.
func (s *Storage) GetFileInfo(fileID int) (fi UserFileInfo, e error) {
	e = s.db.QueryRow(getFileInfo, fileID).Scan(&fi.UserID, &fi.FileName, &fi.LastMod, &fi.ChunkCount, &fi.FileHash)
	if e != nil {
		return
	}

	fi.FileID = fileID
	return
}

// GetMissingChunkNumbersForFile will return a slice of chunk numbers that have
// not been added for a given file.
func (s *Storage) GetMissingChunkNumbersForFile(fileID int) ([]int, error) {
	fi, err := s.GetFileInfo(fileID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get the file information for fileID %d: %v", fileID, err)
	}

	rows, err := s.db.Query(getAllFileChunksByID, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all of the file chunks from the database for fileID %d: %v", fileID, err)
	}
	defer rows.Close()

	knownChunks := []int{}
	for rows.Next() {
		var num int
		var hash string
		err := rows.Scan(&num, &hash)
		if err != nil {
			return nil, fmt.Errorf("failed to scan the next row while processing files chunks for fileID %d: %v", fileID, err)
		}
		knownChunks = append(knownChunks, num)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan all of the search results for a username: %v", err)
	}

	// sort the list so that it can be searched
	sort.Ints(knownChunks)
	maxKnown := len(knownChunks)

	// attempt to find each chunk number in the known list and
	// log the ones that are not found.
	mia := []int{}
	for i := 0; i < fi.ChunkCount; i++ {
		if sort.SearchInts(knownChunks, i) >= maxKnown {
			mia = append(mia, i)
		}

	}

	return mia, nil
}

// AddFileChunk adds a binary chunk to storage for a given file at a position in the file
// determined by the chunkNumber passed in and identified by the chunkHash.
func (s *Storage) AddFileChunk(fileID int, chunkNumber int, chunkHash string, chunk []byte) error {
	// sanity check the length of the chunk
	if int64(len(chunk)) > s.ChunkSize {
		return fmt.Errorf("chunk supplied is %d bytes long and the server is using a max size of %d", len(chunk), s.ChunkSize)
	}

	res, err := s.db.Exec(addFileChunk, fileID, chunkNumber, chunkHash, chunk)
	if err != nil {
		return fmt.Errorf("failed to add a new file chunk in the database: %v", err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to add a new file chunk in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to add a new file chunk in the database: %v", err)
	}

	return nil
}

// RemoveFileChunk removes a chunk from storage identifed by the fileID and chunkNumber.
// If the chunkNumber specified is out of range of the file's max chunk count, this will
// simply have no effect. An bool indicating if the chunk was successfully removed is returned
// as well as an error on failure.
func (s *Storage) RemoveFileChunk(fileID int, chunkNumber int) (bool, error) {
	res, err := s.db.Exec(removeFileChunk, fileID, chunkNumber)
	if err != nil {
		return false, fmt.Errorf("failed to remove the file chunk in the database: %v", err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected <= 0 {
		return false, fmt.Errorf("failed to add a new file info in the database; no rows were affected")
	} else if err != nil {
		return false, fmt.Errorf("failed to add a new file info in the database: %v", err)
	}

	return true, nil
}

// FileChunk contains the information stored about a given file chunk.
type FileChunk struct {
	FileID      int
	ChunkNumber int
	ChunkHash   string
	Chunk       []byte
}

// GetFileChunk retrieves a file chunk from storage and returns it. An error value
// is returned on failure.
func (s *Storage) GetFileChunk(fileID int, chunkNumber int) (fc FileChunk, e error) {
	fc.FileID = fileID
	fc.ChunkNumber = chunkNumber

	e = s.db.QueryRow(getFileChunk, fileID, chunkNumber).Scan(&fc.ChunkHash, &fc.Chunk)
	return
}

// getRowCount is a method to return the number of rows for a given table.
func (s *Storage) getRowCount(table string) (int, error) {
	rows, err := s.db.Query("SELECT Count(*) FROM " + table)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		err = rows.Scan(&count)
		if err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("failed to scan all of the search results for the row cound: %v", err)
	}

	return count, nil
}

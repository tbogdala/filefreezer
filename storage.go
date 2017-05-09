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
		Name		TEXT	UNIQUE		NOT NULL ON CONFLICT ABORT,
		Salt		TEXT				NOT NULL,
		Password	BLOB				NOT NULL
	);`

	createUserStatsTable = `CREATE TABLE UserStats (
		UserID 		INTEGER PRIMARY KEY	NOT NULL,
		Quota		INTEGER				NOT NULL,
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
	updateUser       = `UPDATE Users SET Salt = ?, Password = ? WHERE UserID = ?;`

	setUserStats    = `INSERT OR REPLACE INTO UserStats (UserID, Quota, Allocated, Revision) VALUES (?, ?, ?, ?);`
	getUserStats    = `SELECT Quota, Allocated, Revision FROM UserStats WHERE UserID = ?;`
	updateUserStats = `UPDATE UserStats SET Allocated = Allocated + (?), Revision = Revision + 1 WHERE UserID = ?;`
	setUserQuota    = `UPDATE UserStats SET Quota = (?) WHERE UserID = ?;`

	addFileInfo = `INSERT INTO FileInfo (UserID, FileName, LastMod, ChunkCount, FileHash) SELECT ?, ?, ?, ?, ?
						WHERE NOT EXISTS (SELECT 1 FROM FileInfo WHERE UserID = ? AND FileName = ?);`
	getFileInfo        = `SELECT UserID, FileName, LastMod, ChunkCount, FileHash FROM FileInfo WHERE FileID = ?;`
	getFileInfoByName  = `SELECT FileID, LastMod, ChunkCount, FileHash FROM FileInfo WHERE FileName = ? AND UserID = ?;`
	getFileInfoOwner   = `SELECT UserID  FROM FileInfo WHERE FileID = ?;`
	getAllUserFiles    = `SELECT FileID, FileName, LastMod, ChunkCount, FileHash FROM FileInfo WHERE UserID = ?;`
	removeFileInfoByID = `DELETE FROM FileInfo WHERE FileID = ?;`

	getAllFileChunksByID = `SELECT ChunkNum, ChunkHash FROM FileChunks WHERE FileID = ?;`
	addFileChunk         = `INSERT OR REPLACE INTO FileChunks (FileID, ChunkNum, ChunkHash, Chunk) 
							  VALUES (?, ?, ?, ?);`
	removeAllFileChunks = `DELETE FROM FileChunks WHERE FileID = ?;`
	removeFileChunk     = `DELETE FROM FileChunks WHERE FileID = ? AND ChunkNum = ?;`
	getFileChunk        = `SELECT ChunkHash, Chunk FROM FileChunks WHERE FileID = ? AND ChunkNum = ?;`
)

// FileInfo contains the information stored about a given file for a particular user.
type FileInfo struct {
	UserID     int
	FileID     int
	FileName   string
	LastMod    int64
	ChunkCount int
	FileHash   string
}

// FileChunk contains the information stored about a given file chunk.
type FileChunk struct {
	FileID      int
	ChunkNumber int
	ChunkHash   string
	Chunk       []byte
}

// User contains the basic information stored about a use, but does not
// include current allocation or revision statistics.
type User struct {
	ID         int
	Name       string
	Salt       string
	SaltedHash []byte
}

// UserStats contains the user specific state information to track data usage.
type UserStats struct {
	Quota     int
	Allocated int
	Revision  int
}

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

	_, err = s.db.Exec(createUserStatsTable)
	if err != nil {
		return fmt.Errorf("failed to create the USERSTATS table: %v", err)
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
func (s *Storage) AddUser(username string, salt string, saltedHash []byte, quota int) (*User, error) {
	// insert the user into the table ... username uniqueness is enforced
	// as a sql ON CONFLICT ABORT which will fail the INSERT and return an err here.
	res, err := s.db.Exec(addUser, username, salt, saltedHash)
	if err != nil {
		return nil, fmt.Errorf("failed to insert the new user (%s): %v", username, err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return nil, fmt.Errorf("failed to add a new user in the database; no rows were affected")
	} else if err != nil {
		return nil, fmt.Errorf("failed to add a new user in the database: %v", err)
	}

	insertedID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get the id for the last row inserted while adding a new user into the database: %v", err)
	}

	// generate a new UserFileInfo that contains the ID for the file just added to the database
	u := new(User)
	u.ID = int(insertedID)
	u.Name = username
	u.Salt = salt
	u.SaltedHash = saltedHash

	// with the user added, the user stats row needs to get created with
	// the quota and usage statistics
	err = s.SetUserStats(u.ID, quota, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to set the new user's stats in the database: %v", err)
	}

	return u, nil
}

// GetUser queries the Users table for a given username and returns the associated data.
// If the query fails and error will be returned.
func (s *Storage) GetUser(username string) (*User, error) {
	user := new(User)
	user.Name = username
	err := s.db.QueryRow(getUser, username).Scan(&user.ID, &user.Salt, &user.SaltedHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get the user information from the database: %v", err)
	}

	return user, nil
}

// UpdateUser changes the salt, saltedHash and quota for a given userID. This will fail
// if the userID doesn't exist.
func (s *Storage) UpdateUser(userID int, salt string, saltedHash []byte, quota int) error {
	res, err := s.db.Exec(updateUser, salt, saltedHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update the user (%d): %v", userID, err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to update user in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to update user in the database: %v", err)
	}

	// with the user added, the user stats row needs to get created with
	// the quota and usage statistics
	err = s.SetUserQuota(userID, quota)
	if err != nil {
		return fmt.Errorf("failed to set the user's updated quota in the database: %v", err)
	}

	return nil
}

// SetUserQuota sets the user quota for a user by user id.
func (s *Storage) SetUserQuota(userID int, quota int) error {
	res, err := s.db.Exec(setUserQuota, quota, userID)
	if err != nil {
		return fmt.Errorf("failed to set the user quota in the database: %v", err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to set the user stats in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to set the user stats in the database: %v", err)
	}

	return nil
}

// SetUserStats sets the user information for a user by user id and is used to
// do the first insertion of the user into the stats table.
func (s *Storage) SetUserStats(userID int, quota int, allocated int, revision int) error {
	res, err := s.db.Exec(setUserStats, userID, quota, allocated, revision)
	if err != nil {
		return fmt.Errorf("failed to set the user stats in the database: %v", err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to set the user stats in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to set the user stats in the database: %v", err)
	}

	return nil
}

// UpdateUserStats increments the user's revision by one and updates the allocated
// byte counter with the new delta.
func (s *Storage) UpdateUserStats(userID int, allocDelta int) error {
	res, err := s.db.Exec(updateUserStats, allocDelta, userID)
	if err != nil {
		return fmt.Errorf("failed to update the user stats in the database: %v", err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to update the user stats in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to update the user stats in the database: %v", err)
	}

	return nil
}

// GetUserStats returns the user information for a user by user id.
func (s *Storage) GetUserStats(userID int) (*UserStats, error) {
	stats := new(UserStats)
	err := s.db.QueryRow(getUserStats, userID).Scan(&stats.Quota, &stats.Allocated, &stats.Revision)
	if err != nil {
		return nil, fmt.Errorf("failed to get the user stats from the database: %v", err)
	}

	return stats, nil
}

// RemoveFile removes a file listing and all of the associated chunks in storage.
// Returns an error on failure
func (s *Storage) RemoveFile(userID, fileID int) error {
	err := s.transact(func(tx *sql.Tx) error {
		// check to make sure the user owns the file id
		var owningUserID int
		err := tx.QueryRow(getFileInfoOwner, fileID).Scan(&owningUserID)
		if err != nil {
			return fmt.Errorf("failed to get the owning user id for a given file: %v", err)
		}
		if owningUserID != userID {
			return fmt.Errorf("user does not own the file id supplied")
		}

		// remove the file info
		_, err = tx.Exec(removeFileInfoByID, fileID)
		if err != nil {
			return fmt.Errorf("failed to remove a file info in the database: %v", err)
		}

		// remove all of the file chunks
		_, err = tx.Exec(removeAllFileChunks, fileID)
		if err != nil {
			return fmt.Errorf("failed to delete the file chunks associated with the file: %v", err)
		}

		// if no rows were affected, that just means there were no chunks that
		// needed to be deleted, so no need to check the result.

		return nil
	})

	return err
}

// RemoveFileInfo removes a file listing in storage, returning an error on failure.
func (s *Storage) RemoveFileInfo(fileID int) error {
	res, err := s.db.Exec(removeFileInfoByID, fileID)
	if err != nil {
		return fmt.Errorf("failed to remove a file info in the database: %v", err)
	}

	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to remove a file info in the database; %d row(s) were affected", affected)
	} else if err != nil {
		return fmt.Errorf("failed to add a new file info in the database: %v", err)
	}

	return nil
}

// AddFileInfo registers a new file for a given user which is identified by the filename string.
// lastmod (time in seconds since 1/1/1970) and the filehash string are provided as well. The
// chunkCount parameter should be the number of chunks required for the size of the file. If the
// file could not be added an error is returned, otherwise nil on success.
func (s *Storage) AddFileInfo(userID int, filename string, lastMod int64, chunkCount int, fileHash string) (*FileInfo, error) {
	res, err := s.db.Exec(addFileInfo, userID, filename, lastMod, chunkCount, fileHash, userID, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to add a new file info in the database: %v", err)
	}

	// make sure one row was affected -- if the file was a duplicate, it violates the SQL command
	// and while an erro wasn't returned above, no rows will be affected.
	affected, err := res.RowsAffected()
	if affected != 1 {
		return nil, fmt.Errorf("failed to add a new file info in the database; no rows were affected (possible duplicate file)")
	} else if err != nil {
		return nil, fmt.Errorf("failed to add a new file info in the database: %v", err)
	}

	insertedID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get the id for the last row inserted while adding a new file info into the database: %v", err)
	}

	// generate a new UserFileInfo that contains the ID for the file just added to the database
	fi := new(FileInfo)
	fi.ChunkCount = chunkCount
	fi.FileHash = fileHash
	fi.FileID = int(insertedID)
	fi.FileName = filename
	fi.LastMod = lastMod
	fi.UserID = userID

	return fi, nil
}

// GetAllUserFileInfos returns a slice of UserFileInfo objects that describe all known
// files in storage for a given user ID. If this query was unsuccessful and error is returned.
func (s *Storage) GetAllUserFileInfos(userID int) ([]FileInfo, error) {
	rows, err := s.db.Query(getAllUserFiles, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get all of the file infos from the database: %v", err)
	}
	defer rows.Close()

	// iterate over the returned rows to create a new slice of file info objects
	result := []FileInfo{}
	for rows.Next() {
		var fi FileInfo
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
func (s *Storage) GetFileInfo(userID int, fileID int) (*FileInfo, error) {
	fi := new(FileInfo)
	fi.FileID = fileID
	err := s.transact(func(tx *sql.Tx) error {
		// check to make sure the user owns the file id
		var owningUserID int
		err := tx.QueryRow(getFileInfoOwner, fileID).Scan(&owningUserID)
		if err != nil {
			return fmt.Errorf("failed to get the owning user id for a given file: %v", err)
		}
		if owningUserID != userID {
			return fmt.Errorf("user does not own the file id supplied")
		}

		err = tx.QueryRow(getFileInfo, fileID).Scan(&fi.UserID, &fi.FileName, &fi.LastMod, &fi.ChunkCount, &fi.FileHash)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return fi, nil
}

// GetFileInfoByName returns a UserFileInfo object that describes the file identified
// by the userID and filename parameters. If this query was unsuccessful an error is returned.
func (s *Storage) GetFileInfoByName(userID int, filename string) (*FileInfo, error) {
	fi := new(FileInfo)
	err := s.db.QueryRow(getFileInfoByName, filename, userID).Scan(&fi.FileID, &fi.LastMod, &fi.ChunkCount, &fi.FileHash)
	if err != nil {
		return nil, err
	}
	fi.UserID = userID
	fi.FileName = filename
	return fi, nil
}

// GetFileChunkInfos returns a slice of FileChunks containing all of the chunk
// information except for the chunk bytes themselves.
func (s *Storage) GetFileChunkInfos(userID int, fileID int) ([]FileChunk, error) {
	var fi FileInfo
	var chunk FileChunk
	knownChunks := []FileChunk{}
	err := s.transact(func(tx *sql.Tx) error {
		// check to make sure the user owns the file id
		var owningUserID int
		err := tx.QueryRow(getFileInfoOwner, fileID).Scan(&owningUserID)
		if err != nil {
			return fmt.Errorf("failed to get the owning user id for a given file: %v", err)
		}
		if owningUserID != userID {
			return fmt.Errorf("user does not own the file id supplied")
		}

		// get the file information
		err = tx.QueryRow(getFileInfo, fileID).Scan(&fi.UserID, &fi.FileName, &fi.LastMod, &fi.ChunkCount, &fi.FileHash)
		if err != nil {
			return err
		}
		fi.FileID = fileID

		// get all of the file chunks for the file
		rows, err := tx.Query(getAllFileChunksByID, fileID)
		if err != nil {
			return fmt.Errorf("failed to get all of the file chunks from the database for fileID %d: %v", fileID, err)
		}
		defer rows.Close()

		chunk.FileID = fileID
		for rows.Next() {
			err := rows.Scan(&chunk.ChunkNumber, &chunk.ChunkHash)
			if err != nil {
				return fmt.Errorf("failed to scan the next row while processing files chunks for fileID %d: %v", fileID, err)
			}
			knownChunks = append(knownChunks, chunk)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to scan all of the search results for a username: %v", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return knownChunks, nil
}

// GetMissingChunkNumbersForFile will return a slice of chunk numbers that have
// not been added for a given file.
func (s *Storage) GetMissingChunkNumbersForFile(userID int, fileID int) ([]int, error) {
	var fi FileInfo
	knownChunks := []int{}
	err := s.transact(func(tx *sql.Tx) error {
		// check to make sure the user owns the file id
		var owningUserID int
		err := tx.QueryRow(getFileInfoOwner, fileID).Scan(&owningUserID)
		if err != nil {
			return fmt.Errorf("failed to get the owning user id for a given file: %v", err)
		}
		if owningUserID != userID {
			return fmt.Errorf("user does not own the file id supplied")
		}

		// get the file information
		err = tx.QueryRow(getFileInfo, fileID).Scan(&fi.UserID, &fi.FileName, &fi.LastMod, &fi.ChunkCount, &fi.FileHash)
		if err != nil {
			return err
		}
		fi.FileID = fileID

		// get all of the file chunks for the file
		rows, err := tx.Query(getAllFileChunksByID, fileID)
		if err != nil {
			return fmt.Errorf("failed to get all of the file chunks from the database for fileID %d: %v", fileID, err)
		}
		defer rows.Close()

		for rows.Next() {
			var num int
			var hash string
			err := rows.Scan(&num, &hash)
			if err != nil {
				return fmt.Errorf("failed to scan the next row while processing files chunks for fileID %d: %v", fileID, err)
			}
			knownChunks = append(knownChunks, num)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to scan all of the search results for a username: %v", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
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
// determined by the chunkNumber passed in and identified by the chunkHash. The userID is used
// to update the allocation count in the same transaction as well as verify ownership.
func (s *Storage) AddFileChunk(userID int, fileID int, chunkNumber int, chunkHash string, chunk []byte) (*FileChunk, error) {
	chunkLength := int64(len(chunk))

	// sanity check the length of the chunk
	if chunkLength > s.ChunkSize {
		return nil, fmt.Errorf("chunk supplied is %d bytes long and the server is using a max size of %d", len(chunk), s.ChunkSize)
	}

	newChunk := new(FileChunk)
	err := s.transact(func(tx *sql.Tx) error {
		// check to make sure the user owns the file id
		var owningUserID int
		err := tx.QueryRow(getFileInfoOwner, fileID).Scan(&owningUserID)
		if err != nil {
			return fmt.Errorf("failed to get the owning user id for a given file: %v", err)
		}
		if owningUserID != userID {
			return fmt.Errorf("user does not own the file id supplied")
		}

		// get the user's quota fand allocation count and test for a voliation
		var quota, allocated, revision int64
		err = tx.QueryRow(getUserStats, userID).Scan(&quota, &allocated, &revision)
		if err != nil {
			return fmt.Errorf("failed to get the user quota from the database before adding file chunk: %v", err)
		}

		// fail the transaction if there's not enough allocation space
		if (quota - allocated) < chunkLength {
			return fmt.Errorf("not enough free allocation space (quota: %d ; current allocation %d ; chunk size %d)", quota, allocated, chunkLength)
		}

		// now the that prechecks have succeeded, add the file
		res, err := tx.Exec(addFileChunk, fileID, chunkNumber, chunkHash, chunk)
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

		// update the allocation count
		res, err = tx.Exec(updateUserStats, chunkLength, userID)
		if err != nil {
			return fmt.Errorf("failed to update the allocated bytes in the database after adding a chunk: %v", err)
		}
		// make sure one row was affected with the UPDATE statement
		affected, err = res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to update the user info in the database after adding a chunk; no rows were affected")
		} else if err != nil {
			return fmt.Errorf("failed to update the user info in the database after adding a chunk: %v", err)
		}

		newChunk.FileID = fileID
		newChunk.ChunkNumber = chunkNumber
		newChunk.ChunkHash = chunkHash
		newChunk.Chunk = chunk
		return nil
	})

	// return the error, if any, from running the transaction
	if err != nil {
		return nil, err
	}
	return newChunk, nil
}

// RemoveFileChunk removes a chunk from storage identifed by the fileID and chunkNumber.
// If the chunkNumber specified is out of range of the file's max chunk count, this will
// simply have no effect. An bool indicating if the chunk was successfully removed is returned
// as well as an error on failure. userID is required so that the allocation count can updated
// in the same transaction as well as to verify ownership of the chunk.
func (s *Storage) RemoveFileChunk(userID int, fileID int, chunkNumber int) (bool, error) {
	err := s.transact(func(tx *sql.Tx) error {
		// check to make sure the user owns the file id
		var owningUserID int
		err := tx.QueryRow(getFileInfoOwner, fileID).Scan(&owningUserID)
		if err != nil {
			return fmt.Errorf("failed to get the owning user id for a given file: %v", err)
		}
		if owningUserID != userID {
			return fmt.Errorf("user does not own the file id supplied")
		}

		// get the existing chunk so that we can caluclate the chunk size in bytes to
		// remove from the user's allocation count
		var chunkHash string
		var chunk []byte
		err = tx.QueryRow(getFileChunk, fileID, chunkNumber).Scan(&chunkHash, &chunk)
		if err != nil {
			return fmt.Errorf("failed to get the existing chunk before removal: %v", err)
		}
		allocationCount := len(chunk)

		// remove the chunk from the table
		res, err := tx.Exec(removeFileChunk, fileID, chunkNumber)
		if err != nil {
			return fmt.Errorf("failed to remove the file chunk in the database: %v", err)
		}

		// make sure one row was affected
		affected, err := res.RowsAffected()
		if affected <= 0 {
			return fmt.Errorf("failed to add a new file info in the database; no rows were affected")
		} else if err != nil {
			return fmt.Errorf("failed to add a new file info in the database: %v", err)
		}

		// update the allocation counts
		res, err = tx.Exec(updateUserStats, -allocationCount, userID)
		if err != nil {
			return fmt.Errorf("failed to update the allocated bytes in the database after removing a chunk: %v", err)
		}

		// make sure one row was affected with the UPDATE statement
		affected, err = res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to update the user info in the database after removing a chunk; no rows were affected")
		} else if err != nil {
			return fmt.Errorf("failed to update the user info in the database after removing a chunk: %v", err)
		}

		return nil
	})

	// return the error, if any, from running the transaction
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetFileChunk retrieves a file chunk from storage and returns it. An error value
// is returned on failure.
func (s *Storage) GetFileChunk(fileID int, chunkNumber int) (fc *FileChunk, e error) {
	fc = new(FileChunk)
	fc.FileID = fileID
	fc.ChunkNumber = chunkNumber

	e = s.db.QueryRow(getFileChunk, fileID, chunkNumber).Scan(&fc.ChunkHash, &fc.Chunk)
	return
}

// transact takes a function parameter that will get executed within the context
// of a database/sql.DB transaction. This transaction will Comit or Rollback
// based on whether or not an error or panic was generated from this function.
func (s *Storage) transact(transFoo func(*sql.Tx) error) (err error) {
	// start the transaction
	tx, err := s.db.Begin()
	if err != nil {
		return
	}

	defer func() {
		// attempt to recover from a panic and set the error accordingly
		if p := recover(); p != nil {
			switch p := p.(type) {
			case error:
				err = p
			default:
				err = fmt.Errorf("panic: %s", p)
			}
		}

		// if there was an error, we rollback the transaction
		if err != nil {
			tx.Rollback()
			return
		}

		// no error, so run the commit and return the result
		err = tx.Commit()
	}()

	// run the transaction function and do the commit/rollback in the deferred
	// function above
	err = transFoo(tx)
	return err
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

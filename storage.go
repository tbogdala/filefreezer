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
	// CurrentDBVersion is set to the current database version and is used
	// by filefreezer to detect when the database tables need to get updated.
	CurrentDBVersion = 1
)

const (
	createAppDataTable = `CREATE TABLE IF NOT EXISTS AppData (
		DBVersion	INTEGER				NOT NULL
	);`

	createUsersTable = `CREATE TABLE IF NOT EXISTS Users (
        UserID 		INTEGER PRIMARY KEY	NOT NULL,
        Name		TEXT	UNIQUE		NOT NULL ON CONFLICT ABORT,
		Salt		TEXT				NOT NULL,
		Password	BLOB				NOT NULL,
		CryptoHash  BLOB                
    );`

	createUserStatsTable = `CREATE TABLE IF NOT EXISTS UserStats (
        UserID 		INTEGER PRIMARY KEY	NOT NULL,
        Quota		INTEGER				NOT NULL,
        Allocated	INTEGER				NOT NULL,
        Revision	INTEGER				NOT NULL
    );`

	createFileInfoTable = `CREATE TABLE IF NOT EXISTS FileInfo (
        FileID 	          INTEGER PRIMARY KEY  NOT NULL,
        UserID 		      INTEGER              NOT NULL,
        FileName	      TEXT                 NOT NULL,
        IsDir             INTEGER              NOT NULL,
        CurrentVersionID  INTEGER              NOT NULL
      );`

	createFileVersionTable = `CREATE TABLE IF NOT EXISTS FileVersion (
        VersionID   INTEGER PRIMARY KEY	NOT NULL,
        FileID 	    INTEGER 			NOT NULL,
        VersionNum 	INTEGER 			NOT NULL,
        Perms       INTEGER             NOT NULL,
        LastMod		INTEGER				NOT NULL,
        ChunkCount  INTEGER				NOT NULL,
        FileHash	TEXT				NOT NULL
    );`

	createFileChunksTable = `CREATE TABLE IF NOT EXISTS FileChunks (
        ChunkID     INTEGER PRIMARY KEY	NOT NULL,
        FileID 		INTEGER             NOT NULL,
        VersionID   INTEGER             NOT NULL,
        ChunkNum	INTEGER 			NOT NULL,
        ChunkHash	TEXT				NOT NULL,
        Chunk		BLOB				NOT NULL
	);`

	getAppDBVersion = `SELECT DBVersion FROM AppData;`
	setAppDBVersion = `INSERT OR REPLACE INTO AppData (DBVersion) VALUES (?);`

	lookupUserByName  = `SELECT Name FROM Users WHERE Name = ?;`
	addUser           = `INSERT INTO Users (Name, Salt, Password) VALUES (?, ?, ?);`
	getUser           = `SELECT UserID, Salt, Password, CryptoHash FROM Users  WHERE Name = ?;`
	setUserCryptoHash = `UPDATE Users SET CryptoHash = (?) WHERE UserID = ?;`
	updateUser        = `UPDATE Users SET Name = ?, Salt = ?, Password = ?, CryptoHash = ? WHERE UserID = ?;`

	setUserStats    = `INSERT OR REPLACE INTO UserStats (UserID, Quota, Allocated, Revision) VALUES (?, ?, ?, ?);`
	getUserStats    = `SELECT Quota, Allocated, Revision FROM UserStats WHERE UserID = ?;`
	updateUserStats = `UPDATE UserStats SET Allocated = Allocated + (?), Revision = Revision + 1 WHERE UserID = ?;`
	setUserQuota    = `UPDATE UserStats SET Quota = (?) WHERE UserID = ?;`

	addFileInfo = `INSERT INTO FileInfo (UserID, FileName, IsDir, CurrentVersionID) SELECT ?, ?, ?, ?
                        WHERE NOT EXISTS (SELECT 1 FROM FileInfo WHERE UserID = ? AND FileName = ?);`
	getFileInfo           = `SELECT UserID, FileName, IsDir, CurrentVersionID FROM FileInfo WHERE FileID = ?;`
	getFileInfoByName     = `SELECT FileID, IsDir, CurrentVersionID FROM FileInfo WHERE FileName = ? AND UserID = ?;`
	getFileInfoOwner      = `SELECT UserID  FROM FileInfo WHERE FileID = ?;`
	getAllUserFiles       = `SELECT FileID, FileName, IsDir, CurrentVersionID FROM FileInfo WHERE UserID = ?;`
	removeFileInfoByID    = `DELETE FROM FileInfo WHERE FileID = ?;`
	setFileCurrentVersion = `UPDATE FileInfo SET CurrentVersionID = ? WHERE FileID = ?;`

	addFileVersion                = `INSERT INTO FileVersion (FileID, VersionNum, Perms, LastMod, ChunkCount, FileHash) VALUES (?, ?, ?, ?, ?, ?);`
	getFileVersionByID            = `SELECT VersionNum, Perms, LastMod, ChunkCount, FileHash FROM FileVersion WHERE VersionID = ?;`
	removeAllFileVersionsByFileID = `DELETE FROM FileVersion WHERE FileID = ?;`
	removeFileVersionsByFileID    = `DELETE FROM FileVersion WHERE FileID = ? AND (VersionNum BETWEEN ? AND ?);`
	getVersionsForFile            = `SELECT VersionID, VersionNum, Perms, LastMod, ChunkCount, FileHash FROM FileVersion WHERE FileID = ?;`
	getVersionsCountForFile       = `SELECT COUNT(*) AS COUNT FROM FileVersion WHERE FileID = ? AND (VersionNum BETWEEN ? AND ?);`
	getFileVersionsTotalChunkSize = `SELECT SUM(LENGTH(Chunk)) FROM FileChunks 
					INNER JOIN FileVersion on FileChunks.VersionID = FileVersion.VersionID
					WHERE FileChunks.FileID = ? AND (VersionNum BETWEEN ? AND ?);`
	removeAllFileVersionChunks = `DELETE FROM FileChunks
					WHERE ChunkID in (
						SELECT ChunkID FROM FileChunks
						INNER JOIN FileVersion on FileChunks.VersionID = FileVersion.VersionID
						WHERE FileChunks.FileID = ? AND (VersionNum BETWEEN ? AND ?)
					);`

	getAllFileChunksByID  = `SELECT ChunkNum, ChunkHash FROM FileChunks WHERE FileID = ? AND VersionID = ?;`
	addFileChunk          = `INSERT OR REPLACE INTO FileChunks (FileID, VersionID, ChunkNum, ChunkHash, Chunk) VALUES (?, ?, ?, ?, ?);`
	removeAllFileChunks   = `DELETE FROM FileChunks WHERE FileID = ?;`
	removeFileChunk       = `DELETE FROM FileChunks WHERE FileID = ? AND VersionID = ? AND ChunkNum = ?;`
	getFileChunk          = `SELECT ChunkHash, Chunk FROM FileChunks WHERE FileID = ? AND VersionID = ? AND ChunkNum = ?;`
	getFileTotalChunkSize = `SELECT SUM(LENGTH(Chunk)) FROM FileChunks WHERE FileID = ?;`
	getNumberOfFileChunks = `SELECT COUNT(*) AS COUNT FROM FileChunks WHERE FileID = ?;`

	removeUser = `DELETE FROM FileChunks WHERE FileID IN (SELECT FileID FROM FileInfo WHERE UserID = ?);
		DELETE FROM FileVersion WHERE FileID IN (SELECT FileID FROM FileInfo WHERE UserID = ?);
		DELETE FROM FileInfo WHERE UserID = ?;
        DELETE FROM UserStats WHERE UserID = ?;
        DELETE FROM Users WHERE UserID = ?;`
)

// FileInfo contains the information stored about a given file for a particular user.
type FileInfo struct {
	UserID         int
	FileID         int
	FileName       string
	IsDir          bool
	CurrentVersion FileVersionInfo
}

// FileVersionInfo contains the version-specific information for a given file.
type FileVersionInfo struct {
	VersionID     int
	VersionNumber int
	Permissions   uint32
	LastMod       int64
	ChunkCount    int
	FileHash      string
}

// FileChunk contains the information stored about a given file chunk.
type FileChunk struct {
	FileID      int
	VersionID   int
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
	CryptoHash []byte // a bcrypt hash used to verify the bcrypt hash of the crypto password
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

	// enable write-ahead logging for sqlite tx journaling
	// (about 5x write perf increase for smaller writes for databases on the filesystem)
	_, err = db.Exec("PRAGMA main.journal_mode=WAL;")
	if err != nil {
		return nil, fmt.Errorf("Failed to set the journal_mode pragma: %v", err)
	}

	// enable NORMAL mode instead of the default FULL mode for fs sync
	// (about 30x write perf increase for smaller writes for databases on the filesystem)
	_, err = db.Exec("PRAGMA main.synchronous=NORMAL;")
	if err != nil {
		return nil, fmt.Errorf("Failed to set the synchronous pragma: %v", err)
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
	_, err := s.db.Exec(createAppDataTable)
	if err != nil {
		return fmt.Errorf("failed to create the APPDATA table: %v", err)
	}

	_, err = s.db.Exec(createUsersTable)
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

	_, err = s.db.Exec(createFileVersionTable)
	if err != nil {
		return fmt.Errorf("failed to create the FILEVERSION table: %v", err)
	}

	_, err = s.db.Exec(createFileChunksTable)
	if err != nil {
		return fmt.Errorf("failed to create the FILECHUNKS table: %v", err)
	}

	// do some initialization if necessary
	// TODO: Update database tables if there's a version bump.
	var dbVersion int
	err = s.db.QueryRow(getAppDBVersion).Scan(&dbVersion)
	if err == sql.ErrNoRows {
		_, err = s.db.Exec(setAppDBVersion, CurrentDBVersion)
		if err != nil {
			return fmt.Errorf("failed to set an initial DBVersion in the AppData table: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to get the DBVersion from the AppData table: %v", err)
	}

	return nil
}

// GetDBVersion will return the DB Version number for the opened database.
func (s *Storage) GetDBVersion() (int, error) {
	var dbVersion int
	err := s.db.QueryRow(getAppDBVersion).Scan(&dbVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to get the DBVersion from the AppData table: %v", err)

	}
	return dbVersion, nil
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
	err := s.db.QueryRow(getUser, username).Scan(&user.ID, &user.Salt, &user.SaltedHash, &user.CryptoHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get the user information from the database: %v", err)
	}

	return user, nil
}

// RemoveUser removes user and all files and file chunks associated with the user.
func (s *Storage) RemoveUser(username string) error {
	// make sure we have a user to begin with
	user, err := s.GetUser(username)
	if err != nil {
		return fmt.Errorf("Failed to find the user in the database: %v", err)
	}

	_, err = s.db.Exec(removeUser, user.ID, user.ID, user.ID, user.ID, user.ID)
	if err != nil {
		return fmt.Errorf("failed to remove the user %s (id: %d): %v", user.Name, user.ID, err)
	}

	return nil
}

// UpdateUserCryptoHash changes the cryptoHash for a given userID.
// This will fail if the userID doesn't exist.
func (s *Storage) UpdateUserCryptoHash(userID int, cryptoHash []byte) error {
	res, err := s.db.Exec(setUserCryptoHash, cryptoHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update the user's cryptohash (%d): %v", userID, err)
	}

	// make sure one row was affected
	affected, err := res.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("failed to update user's cryptohash in the database; no rows were affected")
	} else if err != nil {
		return fmt.Errorf("failed to update user's cryptohash in the database: %v", err)
	}

	return nil
}

// UpdateUser changes the salt, saltedHash, cryptoHash and quota for a given userID.
// This will fail if the userID doesn't exist.
func (s *Storage) UpdateUser(userID int, name string, salt string, saltedHash []byte, cryptoHash []byte, quota int) error {
	res, err := s.db.Exec(updateUser, name, salt, saltedHash, cryptoHash, userID)
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

// RemoveFileVersions will remove any file versions of the file specified by fileID
// that are between the minVersion and maxVersion (inclusive). A non-nil error
// value is returned on failure.
//
// NOTE: supplying a minVersion and maxVersion that does not include any valid
// file versions will end up returning an error.
func (s *Storage) RemoveFileVersions(userID, fileID, minVersion, maxVersion int) error {
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

		// make sure there are versions to remove
		var versionsToRemove int
		err = tx.QueryRow(getVersionsCountForFile, fileID, minVersion, maxVersion).Scan(&versionsToRemove)
		if err != nil {
			return fmt.Errorf("failed to get the number of versions that are within range: %v", err)
		}

		// if we dont have any versions to remove, just return now without an error
		if versionsToRemove < 1 {
			return nil
		}

		// get the total chunk size used by the file versions
		var totalChunkSize int
		err = tx.QueryRow(getFileVersionsTotalChunkSize, fileID, minVersion, maxVersion).Scan(&totalChunkSize)
		if err != nil {
			return fmt.Errorf("failed to get the chunk sizes for a file in the database: %v", err)
		}

		// remove all of the file chunks used by the file versions
		_, err = tx.Exec(removeAllFileVersionChunks, fileID, minVersion, maxVersion)
		if err != nil {
			return fmt.Errorf("failed to delete the file chunks associated with the file: %v", err)
		}

		// update the allocation counts
		if totalChunkSize > 0 {
			res, err := tx.Exec(updateUserStats, -totalChunkSize, userID)
			if err != nil {
				return fmt.Errorf("failed to update the allocated bytes in the database after removing chunks: %v", err)
			}

			// make sure one row was affected with the UPDATE statement
			affected, err := res.RowsAffected()
			if affected != 1 {
				return fmt.Errorf("failed to update the user info in the database after removing chunks; no rows were affected")
			} else if err != nil {
				return fmt.Errorf("failed to update the user info in the database after removing chunks: %v", err)
			}

			// if no rows were affected, that just means there were no chunks that
			// needed to be deleted, so no need to check the result.
		}

		// remove the file versions
		_, err = tx.Exec(removeFileVersionsByFileID, fileID, minVersion, maxVersion)
		if err != nil {
			return fmt.Errorf("failed to remove the file versions in the database: %v", err)
		}

		return nil
	})

	return err
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

		// remove the file versions
		_, err = tx.Exec(removeAllFileVersionsByFileID, fileID)
		if err != nil {
			return fmt.Errorf("failed to remove the file versions in the database: %v", err)
		}

		// check to see if we have file chunks associated with this file -- which
		// you will not have if the file is empty or the chunks have not been uploaded yet.
		var totalChunkCount int
		err = tx.QueryRow(getNumberOfFileChunks, fileID).Scan(&totalChunkCount)
		if err != nil {
			return fmt.Errorf("failed to get the chunk count for a file in the database: %v", err)
		}

		// get the total size for all chunks attached to the file id
		var totalChunkSize int
		if totalChunkCount > 0 {
			err = tx.QueryRow(getFileTotalChunkSize, fileID).Scan(&totalChunkSize)
			if err != nil {
				return fmt.Errorf("failed to get the chunk sizes for a file in the database: %v", err)
			}

			// remove all of the file chunks
			_, err = tx.Exec(removeAllFileChunks, fileID)
			if err != nil {
				return fmt.Errorf("failed to delete the file chunks associated with the file: %v", err)
			}

			// update the allocation counts
			if totalChunkSize > 0 {
				res, err := tx.Exec(updateUserStats, -totalChunkSize, userID)
				if err != nil {
					return fmt.Errorf("failed to update the allocated bytes in the database after removing chunks: %v", err)
				}

				// make sure one row was affected with the UPDATE statement
				affected, err := res.RowsAffected()
				if affected != 1 {
					return fmt.Errorf("failed to update the user info in the database after removing chunks; no rows were affected")
				} else if err != nil {
					return fmt.Errorf("failed to update the user info in the database after removing chunks: %v", err)
				}

				// if no rows were affected, that just means there were no chunks that
				// needed to be deleted, so no need to check the result.
			}
		}

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
func (s *Storage) AddFileInfo(userID int, filename string, isDir bool, permissions uint32, lastMod int64, chunkCount int, fileHash string) (*FileInfo, error) {
	fi := new(FileInfo)

	const newVersionNumber = 1

	err := s.transact(func(tx *sql.Tx) error {
		// attempt to first add to the FileInfo table
		res, err := tx.Exec(addFileInfo, userID, filename, isDir, newVersionNumber, userID, filename)
		if err != nil {
			return fmt.Errorf("failed to add a new file info in the database: %v", err)
		}

		// make sure one row was affected -- if the file was a duplicate, it violates the SQL command
		// and while an erro wasn't returned above, no rows will be affected.
		affected, err := res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to add a new file info in the database; no rows were affected (possible duplicate file)")
		} else if err != nil {
			return fmt.Errorf("failed to add a new file info in the database; error getting rows affected: %v", err)
		}

		newFileID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get the id for the last row inserted while adding a new file info into the database: %v", err)
		}

		// now create a new FileVersion entry
		res, err = tx.Exec(addFileVersion, newFileID, newVersionNumber, permissions, lastMod, chunkCount, fileHash)
		if err != nil {
			return fmt.Errorf("failed to add a new file version in the database: %v", err)
		}

		// make sure only one row was affected
		affected, err = res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to add a new file version in the database; no rows were affected (possible duplicate file)")
		} else if err != nil {
			return fmt.Errorf("failed to add a new file version in the database: %v", err)
		}

		newVersionID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get the id for the last row inserted while adding a new file version into the database: %v", err)
		}

		// update the original new file info object with the versionID just created
		res, err = tx.Exec(setFileCurrentVersion, newVersionID, newFileID)
		if err != nil {
			return fmt.Errorf("failed to update the new file version in the database: %v", err)
		}

		affected, err = res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to update the new file version in the database; no rows were affected (possible duplicate file)")
		} else if err != nil {
			return fmt.Errorf("failed to update the new file version in the database: %v", err)
		}

		// generate a new UserFileInfo that contains the ID for the file just added to the database
		fi.FileID = int(newFileID)
		fi.UserID = userID
		fi.FileName = filename
		fi.IsDir = isDir

		fi.CurrentVersion.VersionID = int(newVersionID)
		fi.CurrentVersion.VersionNumber = newVersionNumber
		fi.CurrentVersion.Permissions = permissions
		fi.CurrentVersion.LastMod = lastMod
		fi.CurrentVersion.ChunkCount = chunkCount
		fi.CurrentVersion.FileHash = fileHash

		return nil
	})

	// if the tx failed, then return here
	if err != nil {
		return nil, err
	}

	return fi, nil
}

// GetAllUserFileInfos returns a slice of UserFileInfo objects that describe all known
// files in storage for a given user ID. If this query was unsuccessful and error is returned.
func (s *Storage) GetAllUserFileInfos(userID int) ([]FileInfo, error) {
	var result []FileInfo
	err := s.transact(func(tx *sql.Tx) error {
		rows, err := tx.Query(getAllUserFiles, userID)
		if err != nil {
			return fmt.Errorf("failed to get all of the file infos from the database: %v", err)
		}
		defer rows.Close()

		// iterate over the returned rows to create a new slice of file info objects
		allFileInfos := []FileInfo{}
		for rows.Next() {
			var fi FileInfo
			err := rows.Scan(&fi.FileID, &fi.FileName, &fi.IsDir, &fi.CurrentVersion.VersionID)
			if err != nil {
				return fmt.Errorf("failed to scan the next row while processing user file infos: %v", err)
			}
			fi.UserID = userID
			allFileInfos = append(allFileInfos, fi)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to scan all of the search results for a user's file infos: %v", err)
		}

		// an early Close() call on the result which should be harmless
		rows.Close()

		// now that the base of the FileInfo slice is built, iterate over it and pull the current version data
		result = make([]FileInfo, 0, len(allFileInfos))
		for _, fi := range allFileInfos {
			err = tx.QueryRow(getFileVersionByID, fi.CurrentVersion.VersionID).Scan(&fi.CurrentVersion.VersionNumber,
				&fi.CurrentVersion.Permissions, &fi.CurrentVersion.LastMod, &fi.CurrentVersion.ChunkCount, &fi.CurrentVersion.FileHash)
			if err != nil {
				return fmt.Errorf("failed to get the current file version the database: %v", err)
			}

			result = append(result, fi)
		}

		return nil
	})

	// if the tx failed, then return here
	if err != nil {
		return nil, err
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

		// pull the basic file information
		err = tx.QueryRow(getFileInfo, fileID).Scan(&fi.UserID, &fi.FileName, &fi.IsDir, &fi.CurrentVersion.VersionID)
		if err != nil {
			return fmt.Errorf("failed to get the current file info the database: %v", err)
		}

		// pull the current version data
		err = tx.QueryRow(getFileVersionByID, fi.CurrentVersion.VersionID).Scan(&fi.CurrentVersion.VersionNumber,
			&fi.CurrentVersion.Permissions, &fi.CurrentVersion.LastMod, &fi.CurrentVersion.ChunkCount, &fi.CurrentVersion.FileHash)
		if err != nil {
			return fmt.Errorf("failed to get the current file version the database: %v", err)
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

	err := s.transact(func(tx *sql.Tx) error {
		// pull the basic file information
		err := tx.QueryRow(getFileInfoByName, filename, userID).Scan(&fi.FileID, &fi.IsDir, &fi.CurrentVersion.VersionID)
		if err != nil {
			return fmt.Errorf("failed to get the current file info the database: %v", err)
		}
		fi.FileName = filename
		fi.UserID = userID

		// pull the current version data
		err = tx.QueryRow(getFileVersionByID, fi.CurrentVersion.VersionID).Scan(&fi.CurrentVersion.VersionNumber,
			&fi.CurrentVersion.Permissions, &fi.CurrentVersion.LastMod, &fi.CurrentVersion.ChunkCount, &fi.CurrentVersion.FileHash)
		if err != nil {
			return fmt.Errorf("failed to get the current file version the database: %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return fi, nil
}

// GetFileVersions will return a slice of FileVersionInfo that encompases all of the
// versions registered for a given file ID.
func (s *Storage) GetFileVersions(fileID int) ([]FileVersionInfo, error) {
	// pull the current version data
	rows, err := s.db.Query(getVersionsForFile, fileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get the file versions for a given file id (%d): %v", fileID, err)
	}
	defer rows.Close()

	result := make([]FileVersionInfo, 0)
	var vi FileVersionInfo
	for rows.Next() {
		err := rows.Scan(&vi.VersionID, &vi.VersionNumber, &vi.Permissions, &vi.LastMod, &vi.ChunkCount, &vi.FileHash)
		if err != nil {
			return nil, fmt.Errorf("failed to scan the next row while processing files versions for fileID %d: %v", fileID, err)
		}
		result = append(result, vi)
	}

	return result, nil
}

// TagNewFileVersion creates a new version of a given file and returns the new version ID
// as well as the incremented file-local version number.
func (s *Storage) TagNewFileVersion(userID int, fileID int, permissions uint32, lastMod int64, chunkCount int, fileHash string) (*FileInfo, error) {
	fi := new(FileInfo)
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
		fi.FileID = fileID
		err = tx.QueryRow(getFileInfo, fi.FileID).Scan(&fi.UserID, &fi.FileName, &fi.IsDir, &fi.CurrentVersion.VersionID)
		if err != nil {
			return err
		}

		// pull the current version data to get the correct chunk count for the current version
		err = tx.QueryRow(getFileVersionByID, fi.CurrentVersion.VersionID).Scan(&fi.CurrentVersion.VersionNumber,
			&fi.CurrentVersion.Permissions, &fi.CurrentVersion.LastMod, &fi.CurrentVersion.ChunkCount, &fi.CurrentVersion.FileHash)
		if err != nil {
			return fmt.Errorf("failed to get the current file version the database: %v", err)
		}

		// increment the file-local version number
		fi.CurrentVersion.VersionNumber++

		// force-update the current version object to match the parameters
		fi.CurrentVersion.Permissions = permissions
		fi.CurrentVersion.LastMod = lastMod
		fi.CurrentVersion.ChunkCount = chunkCount
		fi.CurrentVersion.FileHash = fileHash

		// now create a new FileVersion entry
		res, err := tx.Exec(addFileVersion, fi.FileID, fi.CurrentVersion.VersionNumber, fi.CurrentVersion.Permissions,
			fi.CurrentVersion.LastMod, fi.CurrentVersion.ChunkCount, fi.CurrentVersion.FileHash)
		if err != nil {
			return fmt.Errorf("failed to add a new file version in the database: %v", err)
		}

		// make sure only one row was affected
		affected, err := res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to add a new file version in the database; no rows were affected (possible duplicate file)")
		} else if err != nil {
			return fmt.Errorf("failed to add a new file version in the database: %v", err)
		}

		newVersionID64, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get the id for the last row inserted while adding a new file version into the database: %v", err)
		}
		fi.CurrentVersion.VersionID = int(newVersionID64)

		// update the original file info object with the versionID just created
		res, err = tx.Exec(setFileCurrentVersion, fi.CurrentVersion.VersionID, fi.FileID)
		if err != nil {
			return fmt.Errorf("failed to update the file version (%d) for the file id (%d) in the database: %v",
				fi.CurrentVersion.VersionID, fi.FileID, err)
		}

		affected, err = res.RowsAffected()
		if affected != 1 {
			return fmt.Errorf("failed to update the new file version in the database; no rows were affected (possible duplicate file)")
		} else if err != nil {
			return fmt.Errorf("failed to update the new file version in the database: %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return fi, nil
}

// GetFileChunkInfos returns a slice of FileChunks containing all of the chunk
// information except for the chunk bytes themselves.
func (s *Storage) GetFileChunkInfos(userID int, fileID int, versionID int) ([]FileChunk, error) {
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

		// get all of the file chunks for the file
		rows, err := tx.Query(getAllFileChunksByID, fileID, versionID)
		if err != nil {
			return fmt.Errorf("failed to get all of the file chunks from the database for fileID %d: %v", fileID, err)
		}
		defer rows.Close()

		chunk.FileID = fileID
		chunk.VersionID = versionID
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
		err = tx.QueryRow(getFileInfo, fileID).Scan(&fi.UserID, &fi.FileName, &fi.IsDir, &fi.CurrentVersion.VersionID)
		if err != nil {
			return err
		}
		fi.FileID = fileID

		// pull the current version data to get the correct chunk count for the current version
		err = tx.QueryRow(getFileVersionByID, fi.CurrentVersion.VersionID).Scan(&fi.CurrentVersion.VersionNumber,
			&fi.CurrentVersion.Permissions, &fi.CurrentVersion.LastMod, &fi.CurrentVersion.ChunkCount, &fi.CurrentVersion.FileHash)
		if err != nil {
			return fmt.Errorf("failed to get the current file version the database: %v", err)
		}

		// get all of the file chunks for the file
		rows, err := tx.Query(getAllFileChunksByID, fileID, fi.CurrentVersion.VersionID)
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
	for i := 0; i < fi.CurrentVersion.ChunkCount; i++ {
		if sort.SearchInts(knownChunks, i) >= maxKnown {
			mia = append(mia, i)
		}

	}

	return mia, nil
}

// AddFileChunk adds a binary chunk to storage for a given file at a position in the file
// determined by the chunkNumber passed in and identified by the chunkHash. The userID is used
// to update the allocation count in the same transaction as well as verify ownership.
func (s *Storage) AddFileChunk(userID int, fileID int, versionID int, chunkNumber int, chunkHash string, chunk []byte) (*FileChunk, error) {
	chunkLength := int64(len(chunk))

	// the length of the chunk is no longer sanity checked because it may
	// become larger with extra data needed for cryptography.

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
		res, err := tx.Exec(addFileChunk, fileID, versionID, chunkNumber, chunkHash, chunk)
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
		newChunk.VersionID = versionID
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
func (s *Storage) RemoveFileChunk(userID int, fileID int, versionID int, chunkNumber int) (bool, error) {
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
		err = tx.QueryRow(getFileChunk, fileID, versionID, chunkNumber).Scan(&chunkHash, &chunk)
		if err != nil {
			return fmt.Errorf("failed to get the existing chunk before removal: %v", err)
		}
		allocationCount := len(chunk)

		// remove the chunk from the table
		res, err := tx.Exec(removeFileChunk, fileID, versionID, chunkNumber)
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
func (s *Storage) GetFileChunk(fileID int, chunkNumber int, versionID int) (fc *FileChunk, e error) {
	fc = new(FileChunk)
	fc.FileID = fileID
	fc.VersionID = versionID
	fc.ChunkNumber = chunkNumber

	e = s.db.QueryRow(getFileChunk, fileID, versionID, chunkNumber).Scan(&fc.ChunkHash, &fc.Chunk)
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

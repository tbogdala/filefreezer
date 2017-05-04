File Freezer
============

A simple to deploy cloud file storage system.

Features
--------

**NOTHING!**

**API AND DATABASE STILL ACTIVELY CHANGING!**

Mid-development commit with a lot of work still going into it.

TODO / Notes
------------

* Inspired from a blog post about Dropbox:
  https://blogs.dropbox.com/tech/2014/07/streaming-file-synchronization/

* create users/password via CLI
  * salt created on user password creation
  * bcrypt2 (salt + pw hash) stored in database
  * ref: https://crackstation.net/hashing-security.htm

* setup net/http to use HTTPS

* create user authentication

* server testing considerations
  * make authentication an interface for a dummy test replacement
  * make a filesystem interface dummy for testing

* flag: default quota amount
* flag: file hashing algo
* flag: max chunk size (default 4MB)
* flag: encrypt files
* flag: hash on start instead of just checking mod time

* make sure it only adds files automatically, not symlinks


Workflows
---------

A. startup:

1. c --> s GetWholeFileHashes()
2. c <-- s   returns JSON list of all file paths & file id & mod times &
             hashes & USERINFO rev count
3. compare against local directory


B. file path doesn't exist locally || remote mod time > local mod time || file hash doesn't match:

1. c --> s GetFileHashes(file path)
2. c <-- s   returns compressed JSON array of chunk hashes and the file id
3. loop for all chunk hashes
4.   c --> s GetFileChunk(file id, chunk number)
5.   c <-- s   return chunk binary
6.   write chunk to output stream


C. local file mod time > remote mod time && file hashes don't match || doesn't exist remotely:  

1. c --> s UpdateFile(file path) with JSON of chunk hashes & last mod time & whole file hash
2.   server will update FILEINFO table; update all FILEHASHES;
     if chunk hash doesn't match binary, remove FILECHUNKS row and remove chunk byte count from USERINFO quota;
     update USERINFO revision count
3. c --> s GetFileMissingParts(file path)
4. c <-- s   returns the file id | missing chunk numbers in span pairs (start # .. end #)
5. loop for all updated chunks
6.   c --> s SendFileChunk(file id, chunk number) sending binary
7.             server stores binary in FILECHUNKS and updates USERINFO quota;
8.             if quota is violated, don't store chunk and return ERROR


D. on client timer:

1. c --> s PingRevision()
2. c <-- s   returns USERINFO rev count
3. if rev count doesn't match last tracked one, then repeat step A above
(this can eventually get optimized with a transation log)


E. on client filesystem watcher:

1. if a file mod time changed, repeat steps for C above


Notes: GetFileChunk(id, chunk number) may return nothing if the server doesn't have it yet

Database Requirements
---------------------

* USERS = user id | username | pw hash | salted hash
* USERINFO = user id | bytes  quota | bytes allocated | revision count
* FILEINFO = file id | owning user id | file name (path) | last mod time | chunk count | whole file hash
* FILECHUNKS = file id | chunk number (local to each file [0..]) | chunk hash | chunk binary

Note: the time unit used is the number of seconds elapsed since January 1, 1970 UTC,
or what is known as Time.Unix() in golang.

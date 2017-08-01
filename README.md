File Freezer
============

A simple to deploy cloud file storage system.

Features
--------

**NOTHING!**

**API AND DATABASE STILL ACTIVELY CHANGING!**

Mid-development commit with a lot of work still going into it.

Installation
------------

The following dependencies will need to be installed:

```bash
go get golang.org/x/crypto/bcrypt
go get github.com/dgrijalva/jwt-go
go get github.com/gorilla/mux
go get gopkg.in/alecthomas/kingpin.v2
go get golang.org/x/net/http2
go get github.com/mattn/go-sqlite3
go get github.com/spf13/afero
```

RSA keys for JWT token signing can be created with openssl:

```bash
cd cmd/freezer
ssh-keygen -t rsa -b 4096 -f freezer.rsa
openssl rsa -in freezer.rsa -pubout -outform PEM -out freezer.rsa.pub
```

The self-signed TLS keys for serving HTTP/2 over HTTPS can be created with openssl:

```bash
cd cmd/freezer/certgen
go run generate_cert.go -ca -ecdsa-curve P384 -host 127.0.0.1
mv cert.pem ../freezer.crt
mv key.pem ../freezer.key
```

In production, you'll want to use your own valid certificate public and private keys.


Quick Start (work in progress)
------------------------------

Start up a server in a terminal for a database called `freezer.db`:

```bash
cd cmd/freezer
go build
./freezer adduser -u=admin -p=1234
./freezer serve "127.0.0.1:8080"
```

With the server running you can now execute commands, such as:

```bash
./freezer -u=admin -p=1234 userstats http://127.0.0.1:8080
./freezer -u=admin -p=1234 getfiles http://127.0.0.1:8080
./freezer -u=admin -p=1234 sync http://127.0.0.1:8080 .bashrc /backupcfg
./freezer -u=admin -p=1234 syncdir http://127.0.0.1:8080 ~/Downloads /data
```

Known Bugs and Limitations
--------------------------

* File permissions are not saved. Neither are directory permissions. All
  directories created by a `syncdir` operation get created with 0777 permission.

* Empty directories do not get synced.


TODO / Notes
------------


* Inspired from a blog post about Dropbox:
  https://blogs.dropbox.com/tech/2014/07/streaming-file-synchronization/

* create users/password via CLI
  * salt created on user password creation
  * bcrypt2 (salt + pw hash) stored in database
  * ref: https://crackstation.net/hashing-security.htm

* flag: file hashing algo
* flag: encrypt files
* flag: hash on start instead of just checking mod time
* flag: verbosity level

* Storage needs a rename user function. This needs to be exposed
  through a command in cmd/freezer and added to unit tests in both
  places.

* make sure it only adds files automatically, not symlinks
* work on readability of error messages wrt bubbling up error objects
* Server could return capabilities in json response to login that
  could have things like MaxChunkSize for the client to use.

* When removing a user, any files and chunks owned by the user should
  also be removed.


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

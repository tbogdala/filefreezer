File Freezer
============

A simple to deploy cloud file storage system; Licensed under the GPL v3.

Features
--------

**API AND DATABASE STILL ACTIVELY CHANGING!**

* Zero-knowledge encryption of file data and file name; the server
  does not store the user's cryptography password and cannot decrypt
  any of the data the client sends.

* File versioning

* Multi-user capability with quota restrictions

* Simple data storage backend using Sqlite3


Installation
------------

The following dependencies will need to be installed:

```bash
go get golang.org/x/crypto/bcrypt
go get golang.org/x/crypto/scrypt
go get github.com/dgrijalva/jwt-go
go get github.com/gorilla/mux
go get gopkg.in/alecthomas/kingpin.v2
go get golang.org/x/net/http2
go get github.com/mattn/go-sqlite3
go get github.com/spf13/afero
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
./freezer adduser -u admin -p 1234
./freezer serve ":8081"
```

With the server running you can now execute commands, such as:

```bash
./freezer -u admin -p 1234 -s cryptoPass -h localhost:8081 userstats
./freezer -u admin -p 1234 -s cryptoPass -h localhost:8081 getfiles
./freezer -u admin -p 1234 -s cryptoPass -h localhost:8081 sync .bashrc /backupcfg
./freezer -u admin -p 1234 -s cryptoPass -h localhost:8081 syncdir~/Downloads /data
```

Known Bugs and Limitations
--------------------------

* Empty directories do not get synced. 

* Consider a quota for max fileinfo registered so service cannot be DDOS'd 
  by registering infinite files.

* Incrementing a user's revision number only happens in some areas like chunk modification.
  Consider bumping the revision with new files are added or otherwise changed too.

* userid is taken on some Storage methods, but not all, for checking correct user is accessing data

* cli flag to sync against a particular version

* cli command to purge some/all of stored file versions

* starting a remote sync target name with '/' in win32/msys2 attempts to autocomplete
  the string as a path and not give the desired results.

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
* flag: safetey level for database -- currently it is tuned to be very safe,
  but a non-zero chance of db corruption on power loss or crash. docs for
  sqlite say "in practice, you are more likely to suffer a catastrophic disk failure 
  or some other unrecoverable hardware fault" but this should be tunable
  via command line.

* make sure it only adds files automatically, not symlinks

* work on readability of error messages wrt bubbling up error objects

* condsider bit depth of parameters read in in routes.go (32 vs 64)

* vendor the dependencies; possibly with govendor

* ability to purge version history

* multithreading the chunk uploading of files
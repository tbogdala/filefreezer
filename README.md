File Freezer
============

A simple to deploy cloud file storage system; Licensed under the GPL v3.

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
./freezer adduser -u admin -p 1234
./freezer serve "127.0.0.1:8080"
```

With the server running you can now execute commands, such as:

```bash
./freezer -u admin -p 1234 userstats http://127.0.0.1:8080
./freezer -u admin -p 1234 getfiles http://127.0.0.1:8080
./freezer -u admin -p 1234 sync http://127.0.0.1:8080 .bashrc /backupcfg
./freezer -u admin -p 1234 syncdir http://127.0.0.1:8080 ~/Downloads /data
```

Known Bugs and Limitations
--------------------------

* File permissions are not saved. Neither are directory permissions. All
  directories created by a `syncdir` operation get created with 0777 permission.

* Empty directories do not get synced.

* Consider a quota for max fileinfo registered so service cannot be DDOS'd 
  by registering infinite files.

* Incrementing a user's revision number only happens in some areas like chunk modification.
  Consider bumping the revision with new files are added or otherwise changed too.

* userid is taken on some Storage methods, but not all, for checking correct user is accessing data

* cli flag to sync against a particular version

* cli command to view a list of versions

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

* make sure it only adds files automatically, not symlinks

* work on readability of error messages wrt bubbling up error objects

* condsider bit depth of parameters read in in routes.go (32 vs 64)

File Freezer
============

A simple to deploy cloud file storage multi-user system; Licensed under the GPL v3.

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

The quick way to install file freezer is to use `go get` to download
the repository and its dependences and then `go install` to install
the `freezer` CLI executable to $GOROOT/bin.

```bash
go get github.com/tbogdala/filefreezer/...
go install github.com/tbogdala/filefreezer/cmd/freezer
```

To build the project manually from source code, you will want to vendor the
depenencies used by the project. This process is now managed by Go's 
[dep](https://github.com/golang/dep) tool. Simply run the following 
commands to build the vendor directory for dependencies and then build
the `freezer` CLI executable.

```
cd $GOPATH/src/github.com/tbogdala/filefreezer
dep ensure
cd cmd/freezer
go build
go install
```

To serve HTTPS with self-signed TLS keys for development purposes, the necessary files
can be generated with openssl using the certgen tool from the Go source code:

```bash
cd cmd/freezer/certgen
go run generate_cert.go -ca -ecdsa-curve P384 -host 127.0.0.1
mv cert.pem ../freezer.crt
mv key.pem ../freezer.key
```

In production you will want to use your own valid certificate public and private keys
for serving HTTPS.


Quick Start (work in progress)
------------------------------

Before running the server you must create users for the system or else
no one will be able to authenticate and sync files. The act of adding
a user will also create the database file that will be used later
when running the server.

To setup a user named `admin` with a password of `1234` run the following command:

```bash
freezer user add -u admin -p 1234
```

If you wanted to remove this user, user the following command:

```bash
freezer user rm -u admin
```

At any point you can modify the user information like name, password 
and quota using the `freezer user mod` command. For example you 
can change the quota of the admin user to 1 KB by running the
following command:

```bash
freezer user mod -u admin --quota 1024
```

Once a user has been added to the storage database you can launch
the server listening on port 8080 by running the following command:

```bash
freezer serve ":8080"
```

With the server running you can now check the user's stats with
this command:

```bash
freezer -u admin -p 1234 -h localhost:8080 user stats
```

Before uploading files the client needs to specify a cryptography password
so that all file names and data are encrypted on the client's machine and
only the client has knowledge of this crypto password (unlike the login
password, which can be setup by the service administrator separately).

To set the cryptography password for a client, run the following
which will set the crypto pass to `secret`:

```bash
freezer -u admin -p 1234 -h localhost:8080 user cryptopass secret
```

Since the file names are encrypted as well as the file data, the crypto
password has to be setup before you can see the list of files the user
has synchronized with the server. 

To get the list of files stored by the user, run the following:

```bash
freezer -u admin -p 1234 -h localhost:8080 file ls
```

A file can be syncrhonized with the server by running the following command,
which for test purposes will upload a file called `hello.txt` from the user's
home directory:

```bash
freezer -u admin -p 1234 -s secret -h localhost:8080 sync ~/hello.txt hello.txt
```

The first parameter to the `sync` command is the local filepath to syncrhonize.
A second parameter can be specified to override what the file would be called
on the server. If only `~/hello.txt` was specified, it will get expanded and 
named on the server as `/home/timothy/hello.txt` (depending on the user's home
directory). By providing the second parameter of `hello.txt` it will now be
known as only `hello.txt` on the server.

If at some point you want to remove this file, you can do so with the 
following command:

```bash
freezer -u admin -p 1234 -h localhost:8080 file rm hello.txt
```

Notice that the command takes the name of the file on the server and not
the local file which was originally specified on the command line.

If you make a change to the `~/hello.txt` file and sync again it will upload
a new version of that file to the server.

```bash
freezer -u admin -p 1234 -s secret -h localhost:8080 sync ~/hello.txt hello.txt
```

You can get a list of stored versions on the server for a given file by
running the following command:

```bash
freezer -u admin -p 1234 -s secret -h localhost:8080 file versions hello.txt
```

If you wanted to syncronize the local file back to the first version of the
file, you can do so with the following command which will overrite the local
file with the original version of the file still stored on the server:

```bash
freezer -u admin -p 1234 -s secret -h localhost:8080 sync --version=1 ~/hello.txt hello.txt
```

The local file should now be set back to what it was when it was originally synchronzied.

A shortcut to synchronize an entire directory is this command:

```bash
freezer -u admin -p 1234 -s secret -h localhost:8080 syncdir /etc serverbackup/etc
```

This will upload the entire `/etc` folder and all of its subfolders to the server
under a prefix of `serverbackup`. By using a prefix like this in the target of
a `sync` or `syncdir` operation, you can logically organize different groups of files.


Known Bugs and Limitations
--------------------------

* Consider a quota for max fileinfo registered so service cannot be DDOS'd 
  by registering infinite files.

* Incrementing a user's revision number only happens in some areas like chunk modification.
  Consider bumping the revision with new files are added or otherwise changed too.

* userid is taken on some Storage methods, but not all, for checking correct user is accessing data

* cli command to purge some/all of stored file versions

* starting a remote sync target name with '/' in win32/msys2 attempts to autocomplete
  the string as a path and not give the desired results. the fix is to use cmd.exe to 
  perform the command line execution to get the desired results.


TODO / Notes
------------

* Inspired from a blog post about Dropbox:
  https://blogs.dropbox.com/tech/2014/07/streaming-file-synchronization/

* create users/password via CLI
  * salt created on user password creation
  * bcrypt2 (salt + pw hash) stored in database
  * ref: https://crackstation.net/hashing-security.htm

* flag: file hashing algo
* flag: hash on start instead of just checking mod time
* flag: safetey level for database -- currently it is tuned to be very safe,
  but a non-zero chance of db corruption on power loss or crash. docs for
  sqlite say "in practice, you are more likely to suffer a catastrophic disk failure 
  or some other unrecoverable hardware fault" but this should be tunable
  via command line.

* work on readability of error messages wrt bubbling up error objects

* multithreading the chunk uploading of files
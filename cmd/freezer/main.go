// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.

package main

import (
	"os"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	appFlags           = kingpin.New("freezer", "A web application server for FileFreezer.")
	flagDatabasePath   = appFlags.Flag("db", "Database path").Default("file:freezer.db").String()
	flagPublicKeyPath  = appFlags.Flag("pub", "The file path to the public key").Default("freezer.rsa.pub").String()
	flagPrivateKeyPath = appFlags.Flag("priv", "The file path to the private key").Default("freezer.rsa").String()

	cmdServe      = appFlags.Command("serve", "Adds a new user to the database.")
	argListenAddr = cmdServe.Arg("http", "The net address to listen to").Default(":8080").String()

	cmdAddUser      = appFlags.Command("adduser", "Adds a new user to the database.")
	argAddUserName  = cmdAddUser.Arg("username", "The username for user.").Required().String()
	argAddUserPass  = cmdAddUser.Arg("password", "The password for user.").Required().String()
	argAddUserQuota = cmdAddUser.Arg("quota", "The quota size in bytes.").Default("1000000000").Int()

	cmdModUser      = appFlags.Command("moduser", "Modifies a user in the database.")
	argModUserName  = cmdModUser.Arg("username", "The username for user.").Required().String()
	argModUserPass  = cmdModUser.Arg("password", "The password for user.").Required().String()
	argModUserQuota = cmdModUser.Arg("quota", "The quota size in bytes.").Default("1000000000").Int()
)

func main() {
	switch kingpin.MustParse(appFlags.Parse(os.Args[1:])) {
	case cmdServe.FullCommand():
		// setup a channel to gracefully stop the http server
		runServe(*flagDatabasePath, *flagPublicKeyPath, *flagPrivateKeyPath)

	case cmdAddUser.FullCommand():
		username := *argAddUserName
		password := *argAddUserPass
		quota := *argAddUserQuota
		runAddUser(username, password, quota)

	case cmdModUser.FullCommand():
		username := *argModUserName
		password := *argModUserPass
		quota := *argModUserQuota
		runModUser(username, password, quota)
	}
}

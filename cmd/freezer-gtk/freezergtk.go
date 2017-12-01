// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.
package main

/*

The GTK Filefreezer Client
==========================

Idea list
---------

* Multiple configurations for filefreezer server connections. Requires:
	- username
	- password?
	- cryptopass?
	- name of the server connection (e.g. Webserver, LocalPi, etc...)

* For each connection, a list of directories to sync. Requires:
	- local filepath
	- remote prefix
	- icon for showing sync state

* Dialog box for entering passwords
	- possibly save passwords per session

* Minimize to task tray

* Periodically sync
	- can attempt to use the revision number for the account through the API

* Monitor IO progress and show a progress bar of some kind

* Save the configuration file using bitbucket.org/tshannon/config

*/

import (
	"fmt"
	"log"
	"runtime"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"

	"github.com/tbogdala/filefreezer"
	"github.com/tbogdala/filefreezer/cmd/freezer/command"
)

const (
	gladeFile = "app.glade"

	iconFolderFilepath = "folder.png"
	iconFileFilepath   = "file.png"
)

var (
	imageFolder *gdk.Pixbuf
	imageFile   *gdk.Pixbuf

	// the loaded user configuration file
	userConfig *UserConfig

	// the main applicaiton window for the program
	mainWnd *mainWindow
)

// Be on the safe side and lock the current goroutine to the same
// OS thread to possibly avoid GTK issues.
func init() {
	runtime.LockOSThread()
}

func main() {
	var err error

	// Initialize GTK without parsing any command line arguments.
	gtk.Init(nil)
	initIcons()

	// load the user's configuration file
	userConfig, err = LoadDefaultUserConfigFile()
	if err != nil {
		fmt.Printf("Unable to load the user configuration file: %v\n", err)
		return
	}

	// load the user interface file
	builder, err := gtk.BuilderNew()
	if err != nil {
		fmt.Printf("Unable to create the GTK Builder object: %v\n", err)
		return
	}

	err = builder.AddFromFile(gladeFile)
	if err != nil {
		fmt.Printf("Unable to load GTK user interface file: %v\n", err)
		return
	}

	// setup the main application window
	mainWnd, err = createMainWindow(builder)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// bind all of the necessary events for the window
	mainWindowConnectEvents(mainWnd)
	mainWnd.RefreshServerConnections(userConfig.ServerConnectionInfos)
	mainWnd.Show()

	/*
		// attempt to get at the directory tree model
		dirTree, err := getDirectoryTree(builder)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		dirTreeStore, err := setupDirectoryTree(dirTree)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		level1 := addDirTreeRow(dirTreeStore, nil, imageFolder, "Test Folder 001")
		level2 := addDirTreeRow(dirTreeStore, level1, imageFolder, "Test Folder 002")
		level3 := addDirTreeRow(dirTreeStore, level2, imageFolder, "Test Folder 003")
		level4 := addDirTreeRow(dirTreeStore, level3, imageFolder, "Test Folder 004")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 004")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 005")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 006")
		dirTree.ExpandAll()
	*/

	/*
		var lame func(s string) bool
		lame = func(s string) bool {
			fmt.Println(s)
			//glib.TimeoutAdd(1000, lame, "IdleAdd executed")
			return true
		}
		glib.TimeoutAdd(1000, lame, "IdleAdd executed")
	*/

	if len(userConfig.ServerConnectionInfos) > 0 {
		// before we get the file list for the active connection, make sure that
		// we have the right passwords to use
		cancelOperation := false
		password := userConfig.ServerConnectionInfos[0].Password
		cryptopass := userConfig.ServerConnectionInfos[0].CryptoPass
		if password == "" {
			newPassword, okay, err := RunGetPasswordDialog(builder, mainWnd.wnd, "")
			if err != nil {
				fmt.Printf("failed to show the password dialog: %v\n", err)
				return
			}

			if okay {
				password = newPassword
			} else {
				cancelOperation = true
			}
		}

		if !cancelOperation && cryptopass == "" {
			newPassword, okay, err := RunGetPasswordDialog(builder, mainWnd.wnd, "Enter Cryptography Password")
			if err != nil {
				fmt.Printf("failed to show the crypto password dialog: %v\n", err)
				return
			}

			if okay {
				cryptopass = newPassword
			} else {
				cancelOperation = true
			}
		}

		// start a go routine to connect to the server and retrieve the file
		// list for the user. when the list is retrieved, send it over
		// to the gui via an idle thread handler.
		getFileListOp := func() {
			cmdState := command.NewState()
			connInfo := userConfig.ServerConnectionInfos[0]

			// attempt to get the authentication token
			err = cmdState.Authenticate(connInfo.URL, connInfo.Username, password)
			if err != nil {
				fmt.Printf("Failed to authenticate to the server (%s): %v\n", connInfo.URL, err)
				return
			}

			// make sure the crypto key is correct
			cmdState.CryptoKey, err = filefreezer.VerifyCryptoPassword(cryptopass, string(cmdState.CryptoHash))
			if err != nil {
				fmt.Printf("Failed to set the crypto key for the user: %v\v", err)
				return
			}

			// get all of the files
			allFiles, err := cmdState.GetAllFileHashes()
			if err != nil {
				fmt.Printf("Failed to get the file list from the server: %v\n", err)
				return
			}

			glib.IdleAdd(func(files []filefreezer.FileInfo) bool {
				for _, fi := range files {
					plaintextFilename, err := cmdState.DecryptString(fi.FileName)
					if err != nil {
						fmt.Printf("Failed to decrypt string: %v\n", err)
					} else {
						fmt.Printf("Files: %s\n", plaintextFilename)
					}
				}
				return false
			}, allFiles)
		}

		if !cancelOperation {
			go getFileListOp()
		}
	}

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()
}

// getServerConfigAt returns the configuration at a given index.
// NOTE: this is currently provided as a nicer face to accessing a module-level
// global variable in the main window.
func getServerConfigAt(index int) *ServerConnectInfo {
	if index < 0 || index >= len(userConfig.ServerConnectionInfos) {
		return nil
	}

	return &userConfig.ServerConnectionInfos[index]
}

func mainWindowConnectEvents(wnd *mainWindow) {
	wnd.OnDestroy = func() {
		// before we quit, save the user configuration file
		err := userConfig.Save()
		if err != nil {
			fmt.Printf("Failed to save the user configuration: %v\n", err)
			// no return here, we continue on so we Quit the app.
		}

		gtk.MainQuit()
	}

	wnd.OnAddServerConnection = func(newInfo ServerConnectInfo) {
		// add the connection info to the user configuration file
		userConfig.ServerConnectionInfos = append(userConfig.ServerConnectionInfos, newInfo)

		// reset the combo box with the server friendly names
		wnd.RefreshServerConnections(userConfig.ServerConnectionInfos)
	}

	wnd.OnEditServerConnection = func(index int, newInfo ServerConnectInfo) {
		userConfig.ServerConnectionInfos[index] = newInfo
	}

	wnd.OnRemoveServerConnection = func(activeIndex int) {
		if activeIndex < 0 {
			return // if there's no active item we just return here w/o action
		}

		// remove the item at the activeIndex location in the server connection slice
		if activeIndex == 0 {
			userConfig.ServerConnectionInfos = userConfig.ServerConnectionInfos[1:]
		} else if activeIndex == len(userConfig.ServerConnectionInfos)-1 {
			userConfig.ServerConnectionInfos = userConfig.ServerConnectionInfos[:activeIndex]
		} else {
			userConfig.ServerConnectionInfos = append(
				userConfig.ServerConnectionInfos[:activeIndex],
				userConfig.ServerConnectionInfos[activeIndex+1:]...)
		}

		// reset the combo box with the server friendly names
		wnd.RefreshServerConnections(userConfig.ServerConnectionInfos)
	}
}

func initIcons() {
	var err error
	imageFolder, err = gdk.PixbufNewFromFile(iconFolderFilepath)
	if err != nil {
		log.Fatal("Unable to load folder icon:", err)
	}

	imageFile, err = gdk.PixbufNewFromFile(iconFileFilepath)
	if err != nil {
		log.Fatal("Unable to load file icon:", err)
	}

	return
}

/*
// a nil iter adds a root node to the tree
func addDirTreeRow(treeStore *gtk.TreeStore, iter *gtk.TreeIter, icon *gdk.Pixbuf, text string) *gtk.TreeIter {
	// Get an iterator for a new row at the end of the list store
	i := treeStore.Append(iter)

	// Set the contents of the tree store row that the iterator represents
	err := treeStore.SetValue(i, 0, icon)
	if err != nil {
		log.Fatal("Unable set icon:", err)
	}
	err = treeStore.SetValue(i, 1, text)
	if err != nil {
		log.Fatal("Unable set path:", err)
	}

	return i
}


*/

func getBuilderButtonByName(builder *gtk.Builder, name string) (*gtk.Button, error) {
	buttonObj, err := builder.GetObject(name)
	if err != nil {
		return nil, fmt.Errorf("unable to access add server configuration button: %v", err)
	}

	btn, ok := buttonObj.(*gtk.Button)
	if !ok {
		return nil, fmt.Errorf("failed to cast the add server configuration object")
	}

	return btn, nil
}

func getBuilderTextEntryByName(builder *gtk.Builder, name string) (*gtk.Entry, error) {
	obj, err := builder.GetObject(name)
	if err != nil {
		return nil, err
	}

	entry, okay := obj.(*gtk.Entry)
	if !okay {
		return nil, fmt.Errorf("failed to cast the gtk object to an entry control")
	}

	return entry, nil
}

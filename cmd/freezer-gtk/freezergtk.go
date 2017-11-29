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

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeFile = "app.glade"

	gladeAppWindow        = "AppWindow"
	gladeDirTree          = "DirectoryTree"
	gladeStatusBar        = "StatusBar"
	gladeServerConf       = "ServerNameComboBox"
	gladeAddServerConf    = "AddServerConfButton"
	gladeRemoveServerConf = "RemoveServerConfButton"
	gladeAddDirectory     = "AddDirectoryButton"

	iconFolderFilepath = "folder.png"
	iconFileFilepath   = "file.png"
)

var (
	imageFolder *gdk.Pixbuf
	imageFile   *gdk.Pixbuf

	// the loaded user configuration file
	userConfig *UserConfig
)

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
	win, err := getAppWindow(builder)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	win.ShowAll()

	// bind all of the necessary events for the window
	err = mainWindowConnectEvents(builder, win)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// setup the server connections found in the user config
	err = setupServerConnectInfos(builder, userConfig.ServerConnectionInfos)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

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

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()
}

func mainWindowConnectEvents(builder *gtk.Builder, win *gtk.ApplicationWindow) error {
	win.Connect("destroy", func() {
		// before we quit, save the user configuration file
		err := userConfig.Save()
		if err != nil {
			fmt.Printf("Failed to save the user configuration: %v\n", err)
			// no return here, we continue on so we Quit the app.
		}

		gtk.MainQuit()
	})

	addConfBtn, err := getAddServerConfButton(builder)
	if err != nil {
		return err
	}
	addConfBtn.Connect("clicked", func() {
		addConfDlg, err := createAddServerConfDialog(builder, win)
		if err != nil {
			fmt.Printf("Failed to show the add server configuration dialog: %v\n", err)
			return
		}

		retVal := addConfDlg.Run()
		if retVal == int(gtk.RESPONSE_OK) {
			newInfo, err := addConfDlg.GetConnectInfo()
			if err != nil {
				fmt.Printf("Failed to get the connection information from the UI: %v\n", err)
				return
			}

			// add the connection info to the user configuration file
			userConfig.ServerConnectionInfos = append(userConfig.ServerConnectionInfos, newInfo)
		}
	})

	removeConfBtn, err := getRemoveServerConfBuftton(builder)
	if err != nil {
		return err
	}
	removeConfBtn.Connect("clicked", func() {
		combo, err := getServerConfComboBox(builder)
		if err != nil {
			fmt.Printf("Failed to access the server configuration combo box: %v\n", err)
			return
		}

		activeIndex := combo.GetActive()
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
		err = setupServerConnectInfos(builder, userConfig.ServerConnectionInfos)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

	})

	return nil
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

func setupServerConnectInfos(builder *gtk.Builder, infos []ServerConnectInfo) error {
	combo, err := getServerConfComboBox(builder)
	if err != nil {
		return err
	}

	// remove all previous existing items and then ad
	combo.RemoveAll()
	for _, info := range infos {
		combo.AppendText(info.FriendlyName)
	}

	if len(infos) > 0 {
		combo.SetActive(0)
	}

	return nil
}

func setupDirectoryTree(dirTree *gtk.TreeView) (*gtk.TreeStore, error) {
	col, err := gtk.TreeViewColumnNew()
	if err != nil {
		return nil, fmt.Errorf("failed to create a new treeview column: %v", err)
	}

	dirTree.AppendColumn(col)

	iconRenderer, err := gtk.CellRendererPixbufNew()
	if err != nil {
		return nil, fmt.Errorf("unable to create pixbuf cell renderer: %v", err)
	}

	col.PackStart(&iconRenderer.CellRenderer, false)
	col.AddAttribute(iconRenderer, "pixbuf", 0)

	pathRenderer, err := gtk.CellRendererTextNew()
	if err != nil {
		return nil, fmt.Errorf("unable to create text cell renderer: %v", err)
	}

	col.PackStart(&pathRenderer.CellRenderer, true)
	col.AddAttribute(pathRenderer, "text", 1)

	// create the model
	store, err := gtk.TreeStoreNew(glib.TYPE_OBJECT, glib.TYPE_STRING)
	if err != nil {
		return nil, fmt.Errorf("unable to create the treestore: %v", err)
	}
	dirTree.SetModel(store)

	return store, nil
}

func getDirectoryTree(builder *gtk.Builder) (*gtk.TreeView, error) {
	treeObj, err := builder.GetObject(gladeDirTree)
	if err != nil {
		return nil, fmt.Errorf("unable to access directory tree view: %v", err)
	}

	tree, ok := treeObj.(*gtk.TreeView)
	if !ok {
		return nil, fmt.Errorf("failed to cast the directory tree view object")
	}

	return tree, nil
}

func getServerConfComboBox(builder *gtk.Builder) (*gtk.ComboBoxText, error) {
	comboObj, err := builder.GetObject(gladeServerConf)
	if err != nil {
		return nil, fmt.Errorf("unable to access add server configuration button: %v", err)
	}

	combo, ok := comboObj.(*gtk.ComboBoxText)
	if !ok {
		return nil, fmt.Errorf("failed to cast the add server configuration object")
	}

	return combo, nil
}

func getRemoveServerConfBuftton(builder *gtk.Builder) (*gtk.Button, error) {
	return getBuilderButtonByName(builder, gladeRemoveServerConf)
}

func getAddServerConfButton(builder *gtk.Builder) (*gtk.Button, error) {
	return getBuilderButtonByName(builder, gladeAddServerConf)
}

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

func getAppWindow(builder *gtk.Builder) (*gtk.ApplicationWindow, error) {
	winObj, err := builder.GetObject(gladeAppWindow)
	if err != nil {
		return nil, fmt.Errorf("unable to access main application window: %v", err)
	}

	win, ok := winObj.(*gtk.ApplicationWindow)
	if !ok {
		return nil, fmt.Errorf("failed to cast the application window object")
	}

	return win, nil
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

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.
package main

import (
	"fmt"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeAppWindow        = "AppWindow"
	gladeDirTree          = "DirectoryTree"
	gladeStatusbar        = "StatusBar"
	gladeServerConf       = "ServerNameComboBox"
	gladeAddServerConf    = "AddServerConfButton"
	gladeRemoveServerConf = "RemoveServerConfButton"
	gladeAddDirectory     = "AddDirectoryButton"
)

type mainWindow struct {
	builder *gtk.Builder
	wnd     *gtk.ApplicationWindow

	directoryTree      *gtk.TreeView
	statusBar          *gtk.Statusbar
	serverConfCombo    *gtk.ComboBoxText
	addServerButton    *gtk.Button
	removeServerButton *gtk.Button

	OnDestroy                func()
	OnAddServerConnection    func(newInfo ServerConnectInfo)
	OnRemoveServerConnection func(activeIndex int)
}

func createMainWindow(builder *gtk.Builder) (*mainWindow, error) {
	var err error

	// get the window controls
	mainWnd := new(mainWindow)
	mainWnd.builder = builder
	mainWnd.wnd, err = mainWnd.getAppWindow()
	if err != nil {
		return nil, err
	}

	mainWnd.directoryTree, err = mainWnd.getDirectoryTree()
	if err != nil {
		return nil, err
	}

	mainWnd.serverConfCombo, err = mainWnd.getServerConfComboBox()
	if err != nil {
		return nil, err
	}

	mainWnd.addServerButton, err = getBuilderButtonByName(mainWnd.builder, gladeAddServerConf)
	if err != nil {
		return nil, err
	}

	mainWnd.removeServerButton, err = getBuilderButtonByName(mainWnd.builder, gladeRemoveServerConf)
	if err != nil {
		return nil, err
	}

	// connect the event handlers to call our function pointers
	mainWnd.connectEvents()

	return mainWnd, nil
}

// Show displays the main window by showing all the controls.
func (w *mainWindow) Show() {
	w.wnd.ShowAll()
}

func (w *mainWindow) RefreshServerConnections(infos []ServerConnectInfo) {
	// remove all previous existing items and then ad
	w.serverConfCombo.RemoveAll()
	for _, info := range infos {
		w.serverConfCombo.AppendText(info.FriendlyName)
	}

	if len(infos) > 0 {
		w.serverConfCombo.SetActive(0)
	}
}

func (w *mainWindow) getDirectoryTree() (*gtk.TreeView, error) {
	treeObj, err := w.builder.GetObject(gladeDirTree)
	if err != nil {
		return nil, fmt.Errorf("unable to access directory tree view: %v", err)
	}

	tree, ok := treeObj.(*gtk.TreeView)
	if !ok {
		return nil, fmt.Errorf("failed to cast the directory tree view object")
	}

	return tree, nil
}

func (w *mainWindow) getServerConfComboBox() (*gtk.ComboBoxText, error) {
	comboObj, err := w.builder.GetObject(gladeServerConf)
	if err != nil {
		return nil, fmt.Errorf("unable to access add server configuration button: %v", err)
	}

	combo, ok := comboObj.(*gtk.ComboBoxText)
	if !ok {
		return nil, fmt.Errorf("failed to cast the add server configuration object")
	}

	return combo, nil
}

func (w *mainWindow) getAppWindow() (*gtk.ApplicationWindow, error) {
	winObj, err := w.builder.GetObject(gladeAppWindow)
	if err != nil {
		return nil, fmt.Errorf("unable to access main application window: %v", err)
	}

	win, ok := winObj.(*gtk.ApplicationWindow)
	if !ok {
		return nil, fmt.Errorf("failed to cast the application window object")
	}

	return win, nil
}

func (w *mainWindow) connectEvents() {
	w.wnd.Connect("destroy", func() {
		if w.OnDestroy != nil {
			w.OnDestroy()
		}
	})

	w.addServerButton.Connect("clicked", func() {
		// if no event has been setup just return here
		if w.OnAddServerConnection == nil {
			return
		}

		addConfDlg, err := createAddServerConfDialog(w.builder, w.wnd)
		if err != nil {
			fmt.Printf("failed to show the add server configuration dialog: %v", err)
			return
		}

		retVal := addConfDlg.Run()
		if retVal == int(gtk.RESPONSE_OK) {
			newInfo, err := addConfDlg.GetConnectInfo()
			if err != nil {
				fmt.Printf("failed to show the add server configuration dialog: %v", err)
				return
			}

			w.OnAddServerConnection(newInfo)
		}
	})

	w.removeServerButton.Connect("clicked", func() {
		// if no event has ben setup just return here
		if w.OnRemoveServerConnection == nil {
			return
		}

		activeIndex := w.serverConfCombo.GetActive()
		if activeIndex < 0 {
			return // if there's no active item we just return here w/o action
		}

		w.OnRemoveServerConnection(activeIndex)
	})

	return
}

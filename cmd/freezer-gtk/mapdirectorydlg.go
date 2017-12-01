// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.
package main

import (
	"fmt"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeMapDirectoryDlg               = "MapDirectoryDialog"
	gladeMapDirectoryLocalDirChooser   = "MapDirLocalDirChooser"
	gladeMapDirectoryRemotePrefixEntry = "MapDirRemotePrefixEntry"
)

type mapDirectoryDialog struct {
	dlg *gtk.Dialog

	localDirChooser   *gtk.FileChooserButton
	remotePrefixEntry *gtk.Entry
}

func createMapDirectoryDialog(builder *gtk.Builder, parentWin *gtk.ApplicationWindow) (*mapDirectoryDialog, error) {
	dlgObj, err := builder.GetObject(gladeMapDirectoryDlg)
	if err != nil {
		return nil, fmt.Errorf("unable to access the map directory dialog: %v", err)
	}

	dlg, ok := dlgObj.(*gtk.Dialog)
	if !ok {
		return nil, fmt.Errorf("failed to cast the map directory dialog object")
	}

	// set the dialog properties
	dlg.SetTransientFor(parentWin)
	dlg.SetModal(true)

	// get the dialog controls
	result := new(mapDirectoryDialog)
	result.dlg = dlg
	result.remotePrefixEntry, err = getBuilderTextEntryByName(builder, gladeMapDirectoryRemotePrefixEntry)
	if err != nil {
		return nil, err
	}
	localDirObj, err := builder.GetObject(gladeMapDirectoryLocalDirChooser)
	if err != nil {
		return nil, fmt.Errorf("unable to access the local directory chooser: %v", err)
	}

	result.localDirChooser, ok = localDirObj.(*gtk.FileChooserButton)
	if !ok {
		return nil, fmt.Errorf("failed to cast the local directory chooser object")
	}

	// reset the control values
	result.remotePrefixEntry.SetText("")

	return result, nil
}

// Run shows the mapDirectoryDialog box in a modal fashion and returns the result value
// of the dialog box.
func (d *mapDirectoryDialog) Run() int {
	d.dlg.ShowAll()
	retVal := d.dlg.Run()
	d.dlg.Hide()

	return retVal
}

// GetMapping pulls the directory mapping information from the controls and
// returns it in a structure.
func (d *mapDirectoryDialog) GetMapping() (mapping DirectoryMapping, err error) {
	mapping.LocalDirectory = d.localDirChooser.GetFilename()
	mapping.RemotePrefix, err = d.remotePrefixEntry.GetText()
	return mapping, err
}

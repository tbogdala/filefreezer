// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.
package main

import (
	"fmt"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeAddServerConfDlg              = "ServerConfDialog"
	gladeAddServerConfServerName       = "ServerNameText"
	gladeAddServerConfServerURL        = "ServerURLText"
	gladeAddServerConfServerUsername   = "ServerUsernameText"
	gladeAddServerConfServerPassword   = "ServerPasswordText"
	gladeAddServerConfServerCryptoPass = "ServerCryptoPassText"
	gladeAddServerEnableLocalServer    = "RunAsLocalServerCheck"
	gladeAddServerLocalDBFile          = "ServerLocalDBEntry"
	gladeAddServerLocalDBFileLabel     = "ServerLocalDBLabel"
)

type serverConfDialog struct {
	dlg    *gtk.Dialog
	parent *gtk.Window

	serverNameEntry       *gtk.Entry
	serverURLEntry        *gtk.Entry
	serverUsernameEntry   *gtk.Entry
	serverPasswordEntry   *gtk.Entry
	serverCryptoPassEntry *gtk.Entry
	serverEnableLocal     *gtk.CheckButton
	serverLocalDBEntry    *gtk.Entry
	serverLocalDBLabel    *gtk.Label
}

func createServerConfDialog(builder *gtk.Builder, parentWin *gtk.ApplicationWindow) (*serverConfDialog, error) {
	dlgObj, err := builder.GetObject(gladeAddServerConfDlg)
	if err != nil {
		return nil, fmt.Errorf("unable to access the add server configuration dialog: %v", err)
	}

	dlg, ok := dlgObj.(*gtk.Dialog)
	if !ok {
		return nil, fmt.Errorf("failed to cast the add server configuration dialog object")
	}

	// set the dialog properties
	dlg.SetTransientFor(parentWin)
	dlg.SetModal(true)

	// get the dialog controls
	result := new(serverConfDialog)
	result.dlg = dlg
	result.parent = &dlg.Window
	result.serverNameEntry, err = getBuilderTextEntryByName(builder, gladeAddServerConfServerName)
	if err != nil {
		return nil, err
	}
	result.serverURLEntry, err = getBuilderTextEntryByName(builder, gladeAddServerConfServerURL)
	if err != nil {
		return nil, err
	}
	result.serverUsernameEntry, err = getBuilderTextEntryByName(builder, gladeAddServerConfServerUsername)
	if err != nil {
		return nil, err
	}
	result.serverPasswordEntry, err = getBuilderTextEntryByName(builder, gladeAddServerConfServerPassword)
	if err != nil {
		return nil, err
	}
	result.serverCryptoPassEntry, err = getBuilderTextEntryByName(builder, gladeAddServerConfServerCryptoPass)
	if err != nil {
		return nil, err
	}

	enableLocalObj, err := builder.GetObject(gladeAddServerEnableLocalServer)
	if err != nil {
		return nil, fmt.Errorf("unable to access the enable local server checkbox: %v", err)
	}
	result.serverEnableLocal, ok = enableLocalObj.(*gtk.CheckButton)
	if !ok {
		return nil, fmt.Errorf("failed to cast the enable local server checkbox object")
	}

	localDBLabelObj, err := builder.GetObject(gladeAddServerLocalDBFileLabel)
	if err != nil {
		return nil, fmt.Errorf("unable to access the local database file label: %v", err)
	}
	result.serverLocalDBLabel, ok = localDBLabelObj.(*gtk.Label)
	if !ok {
		return nil, fmt.Errorf("failed to cast the local database file chooser object")
	}

	result.serverLocalDBEntry, err = getBuilderTextEntryByName(builder, gladeAddServerLocalDBFile)
	if err != nil {
		return nil, err
	}

	// reset the control values
	result.serverNameEntry.SetText("")
	result.serverURLEntry.SetText("")
	result.serverUsernameEntry.SetText("")
	result.serverPasswordEntry.SetText("")
	result.serverPasswordEntry.SetVisibility(false)
	result.serverCryptoPassEntry.SetText("")
	result.serverCryptoPassEntry.SetVisibility(false)

	result.enableLocalServerControls(false)

	// connect some events
	result.serverEnableLocal.ToggleButton.Connect("clicked", func() {
		localServerEnabled := result.serverEnableLocal.ToggleButton.GetActive()
		result.enableLocalServerControls(localServerEnabled)
	})
	/*()
	result.serverLocalDBBrowse.Connect("clicked", func() {
		fmt.Println("DEBUG CLICKED")
		chooserDlg, err := gtk.FileChooserDialogNewWith1Button(
			"Select Local DB File...",
			nil,
			gtk.FILE_CHOOSER_ACTION_SAVE,
			"Select",
			gtk.RESPONSE_OK)
		if err != nil {
			fmt.Printf("Couldn't create the file chooser dialog for the local DB file: %v\n", err)
			return
		}

		fmt.Println("DEBUG CLICKED 2")
		res := dlg.Run()
		dlg.Hide()
		fmt.Println("DEBUG CLICKED 3")
		if res == int(gtk.RESPONSE_OK) {
			fmt.Println("DEBUG CLICKED 4")
			localFile := chooserDlg.GetFilename()
			result.serverLocalDBEntry.SetText(localFile)
		}
	})
	*/
	return result, nil
}

// Run shows the addServerConfDialog box in a modal fashion and returns the result value
// of the dialog box.
func (d *serverConfDialog) Run() int {
	d.dlg.ShowAll()
	retVal := d.dlg.Run()
	d.dlg.Hide()

	return retVal
}

func (d *serverConfDialog) enableLocalServerControls(enable bool) {
	d.serverEnableLocal.ToggleButton.SetActive(enable)
	if enable {
		d.serverLocalDBEntry.SetSensitive(true)
		d.serverLocalDBLabel.SetSensitive(true)
	} else {
		d.serverLocalDBEntry.SetSensitive(false)
		d.serverLocalDBLabel.SetSensitive(false)
	}
}

// SetConnectInfo sets the dialog controls to the values of the supplied
// ServerConnectInfo parameter.
func (d *serverConfDialog) SetConnectInfo(info ServerConnectInfo) {
	d.serverNameEntry.SetText(info.FriendlyName)
	d.serverURLEntry.SetText(info.URL)
	d.serverUsernameEntry.SetText(info.Username)
	d.serverPasswordEntry.SetText(info.Password)
	d.serverCryptoPassEntry.SetText(info.CryptoPass)

	d.enableLocalServerControls(info.IsLocalServer)
	d.serverLocalDBEntry.SetText(info.LocalServerDB)
}

// GetConnectInfo pulls the user entered data from the view controls
// and populates a new ServerConnectInfo object with that data.
func (d *serverConfDialog) GetConnectInfo() (info ServerConnectInfo, err error) {
	info.FriendlyName, err = d.serverNameEntry.GetText()
	if err != nil {
		return info, err
	}

	info.URL, err = d.serverURLEntry.GetText()
	if err != nil {
		return info, err
	}

	info.Username, err = d.serverUsernameEntry.GetText()
	if err != nil {
		return info, err
	}

	info.Password, err = d.serverPasswordEntry.GetText()
	if err != nil {
		return info, err
	}

	info.CryptoPass, err = d.serverCryptoPassEntry.GetText()
	if err != nil {
		return info, err
	}

	info.IsLocalServer = d.serverEnableLocal.GetActive()
	info.LocalServerDB, err = d.serverLocalDBEntry.GetText()
	if err != nil {
		return info, err
	}

	return info, err
}

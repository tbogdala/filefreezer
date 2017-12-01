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
)

type serverConfDialog struct {
	dlg *gtk.Dialog

	serverNameEntry       *gtk.Entry
	serverURLEntry        *gtk.Entry
	serverUsernameEntry   *gtk.Entry
	serverPasswordEntry   *gtk.Entry
	serverCryptoPassEntry *gtk.Entry
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

	// reset the control values
	result.serverNameEntry.SetText("")
	result.serverURLEntry.SetText("")
	result.serverUsernameEntry.SetText("")
	result.serverPasswordEntry.SetText("")
	result.serverPasswordEntry.SetVisibility(false)
	result.serverCryptoPassEntry.SetText("")
	result.serverCryptoPassEntry.SetVisibility(false)

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

// SetConnectInfo sets the dialog controls to the values of the supplied
// ServerConnectInfo parameter.
func (d *serverConfDialog) SetConnectInfo(info ServerConnectInfo) {
	d.serverNameEntry.SetText(info.FriendlyName)
	d.serverURLEntry.SetText(info.URL)
	d.serverUsernameEntry.SetText(info.Username)
	d.serverPasswordEntry.SetText(info.Password)
	d.serverCryptoPassEntry.SetText(info.CryptoPass)
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

	return info, err
}

// Copyright 2017, Timothy Bogdala <tdb@animal-machine.com>
// See the LICENSE file for more details.
package main

import (
	"fmt"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeGetPasswordDlg  = "GetPasswordDialog"
	gladeGetPasswordText = "GetPasswordText"
)

type getPasswordDialog struct {
	dlg *gtk.Dialog

	passwordEntry *gtk.Entry
}

func createGetPasswordDialog(builder *gtk.Builder, parentWin *gtk.ApplicationWindow) (*getPasswordDialog, error) {
	dlgObj, err := builder.GetObject(gladeGetPasswordDlg)
	if err != nil {
		return nil, fmt.Errorf("unable to access the password dialog: %v", err)
	}

	dlg, ok := dlgObj.(*gtk.Dialog)
	if !ok {
		return nil, fmt.Errorf("failed to cast the password dialog object")
	}

	// set the dialog properties
	dlg.SetTransientFor(parentWin)
	dlg.SetModal(true)

	// get the dialog controls
	result := new(getPasswordDialog)
	result.dlg = dlg
	result.passwordEntry, err = getBuilderTextEntryByName(builder, gladeGetPasswordText)
	if err != nil {
		return nil, err
	}

	// reset the control values
	result.passwordEntry.SetText("")
	result.passwordEntry.SetVisibility(false)
	result.passwordEntry.GrabFocus()

	return result, nil
}

// Run shows the getPasswordDialog box in a modal fashion and returns the result value
// of the dialog box.
func (d *getPasswordDialog) Run() int {
	d.dlg.ShowAll()
	retVal := d.dlg.Run()
	d.dlg.Hide()

	return retVal
}

// RunGetPasswordDialog will create the GetPassword dialog box, show it, check the return
// value of the dialog and if the result was OK then it will return the string in the
// password entry text control and a true bool. If CANCEL was returned from the dialog box
// then a false bool will get returned signifying a canceled operation.
func RunGetPasswordDialog(builder *gtk.Builder, parentWin *gtk.ApplicationWindow, optTitle string) (string, bool, error) {
	getPasswordDlg, err := createGetPasswordDialog(builder, parentWin)
	if err != nil {
		return "", false, fmt.Errorf("failed to show the password dialog: %v", err)
	}

	if optTitle != "" {
		getPasswordDlg.dlg.SetTitle(optTitle)
	}

	retVal := getPasswordDlg.Run()
	if retVal != int(gtk.RESPONSE_OK) {
		return "", false, nil
	}

	newPassword, err := getPasswordDlg.GetPassword()
	if err != nil {
		return "", false, fmt.Errorf("failed to get the password from the password dialog: %v", err)
	}

	return newPassword, true, nil
}

// GetConnectInfo pulls the user entered data from the view controls
// and populates a new ServerConnectInfo object with that data.
func (d *getPasswordDialog) GetPassword() (pw string, err error) {
	pw, err = d.passwordEntry.GetText()
	return pw, err
}

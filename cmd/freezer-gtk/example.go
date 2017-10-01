package main

import (
	"log"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeFile      = "app.glade"
	gladeAppWindow = "AppWindow"
)

func main() {
	// Initialize GTK without parsing any command line arguments.
	gtk.Init(nil)

	// load the user interface file
	builder, err := gtk.BuilderNew()
	if err != nil {
		log.Fatal("Unalbe to create the GTK Builder object:", err)
	}

	err = builder.AddFromFile(gladeFile)
	if err != nil {
		log.Fatal("Unable to load GTK user interface file:", err)
	}

	// setup the main application window
	winObject, err := builder.GetObject(gladeAppWindow)
	if err != nil {
		log.Fatal("Unable to access main application window:", err)
	}
	win, ok := winObject.(*gtk.ApplicationWindow)
	if !ok {
		log.Fatal("Failed to cast the application window object.")
	}
	win.ShowAll()
	win.Connect("destroy", func() {
		gtk.MainQuit()
	})

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()
}

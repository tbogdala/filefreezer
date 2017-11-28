package main

import (
	"fmt"
	"log"

	"github.com/gotk3/gotk3/gdk"
	"github.com/gotk3/gotk3/glib"

	"github.com/gotk3/gotk3/gtk"
)

const (
	gladeFile          = "app.glade"
	gladeAppWindow     = "AppWindow"
	gladeDirTree       = "DirectoryTree"
	iconFolderFilepath = "folder.png"
	iconFileFilepath   = "file.png"
)

var (
	imageFolder *gdk.Pixbuf
	imageFile   *gdk.Pixbuf
)

func main() {
	// Initialize GTK without parsing any command line arguments.
	gtk.Init(nil)
	initIcons()

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
	win, err := getAppWindow(builder)
	if err != nil {
		log.Fatal(err)
	}
	win.ShowAll()
	win.Connect("destroy", func() {
		gtk.MainQuit()
	})

	// attempt to get at the directory tree model
	dirTree, err := getDirectoryTree(builder)
	if err != nil {
		log.Fatal(err)
	}

	dirTreeStore, err := setupDirectoryTree(dirTree)
	if err != nil {
		log.Fatal(err)
	}

	level1 := addDirTreeRow(dirTreeStore, nil, imageFolder, "Test Folder 001")
	level2 := addDirTreeRow(dirTreeStore, level1, imageFolder, "Test Folder 002")
	level3 := addDirTreeRow(dirTreeStore, level2, imageFolder, "Test Folder 003")
	level4 := addDirTreeRow(dirTreeStore, level3, imageFolder, "Test Folder 004")
	addDirTreeRow(dirTreeStore, level4, imageFile, "Test File 004")
	dirTree.ExpandAll()

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()
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

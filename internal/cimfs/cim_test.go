package cimfs

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/hcsshim/osversion"
	"golang.org/x/sys/windows"
)

// A simple tuple type used to hold information about a file/directory that is created
// during a test.
type tuple struct {
	filepath     string
	fileContents []byte
	isDir        bool
}

// A utility function to create a file/directory and write data to it in the given cim
func createCimFileUtil(c *CimFsWriter, fileTuple tuple) error {
	// create files inside the cim
	fileInfo := &winio.FileBasicInfo{
		CreationTime:   windows.NsecToFiletime(time.Now().UnixNano()),
		LastAccessTime: windows.NsecToFiletime(time.Now().UnixNano()),
		LastWriteTime:  windows.NsecToFiletime(time.Now().UnixNano()),
		ChangeTime:     windows.NsecToFiletime(time.Now().UnixNano()),
		FileAttributes: 0,
	}
	if fileTuple.isDir {
		fileInfo.FileAttributes = windows.FILE_ATTRIBUTE_DIRECTORY
	}

	if err := c.AddFile(filepath.FromSlash(fileTuple.filepath), fileInfo, int64(len(fileTuple.fileContents)), []byte{}, []byte{}, []byte{}); err != nil {
		return err
	}

	if !fileTuple.isDir {
		wc, err := c.Write(fileTuple.fileContents)
		if err != nil || wc != len(fileTuple.fileContents) {
			if err == nil {
				return fmt.Errorf("unable to finish writing to file %s", fileTuple.filepath)
			} else {
				return err
			}
		}
	}
	return nil
}

// This test creates a cim, writes some files to it and then reads those files back.
// The cim created by this test has only 3 files in the following tree
// /
// |- foobar.txt
// |- foo
// |--- bar.txt
func TestCimReadWrite(t *testing.T) {
	if osversion.Get().Build < osversion.V21H1 {
		t.Skipf("Requires build %d+", osversion.V21H1)
	}

	testContents := []tuple{
		{"foobar.txt", []byte("foobar test data"), false},
		{"foo", []byte(""), true},
		{"foo\\bar.txt", []byte("bar test data"), false},
	}
	cimName := "test.cim"
	tempDir, err := ioutil.TempDir("", "cim-test")
	if err != nil {
		t.Fatalf("failed while creating temp directory: %s", err)
	}
	defer os.RemoveAll(tempDir)

	c, err := Create(tempDir, "", cimName)
	if err != nil {
		t.Fatalf("failed while creating a cim: %s", err)
	}

	for _, ft := range testContents {
		err := createCimFileUtil(c, ft)
		if err != nil {
			t.Fatalf("failed to create the file %s inside the cim:%s", ft.filepath, err)
		}
	}
	c.Close()

	// open and read the cim
	cimReader, err := Open(filepath.Join(tempDir, cimName))
	if err != nil {
		t.Fatalf("failed while opening the cim: %s", err)
	}

	for _, ft := range testContents {
		// make sure the size of byte array is larger than contents of the largest file
		f, err := cimReader.Open(ft.filepath)
		if err != nil {
			t.Fatalf("unable to read file %s from the cim: %s", ft.filepath, err)
		}
		fileContents := make([]byte, f.Size())
		if !ft.isDir {
			// it is a file - read contents
			rc, err := f.Read(fileContents)
			if err != nil && err != io.EOF {
				t.Fatalf("failure while reading file %s from cim: %s", ft.filepath, err)
			} else if rc != len(ft.fileContents) {
				t.Fatalf("couldn't read complete file contents for file: %s, read %d bytes, expected: %d", ft.filepath, rc, len(ft.fileContents))
			} else if !bytes.Equal(fileContents[:rc], ft.fileContents) {
				t.Fatalf("contents of file %s don't match", ft.filepath)
			}
		} else {
			// it is a directory just do stat
			_, err := f.Stat()
			if err != nil {
				t.Fatalf("failure while reading directory %s from cim: %s", ft.filepath, err)
			}
		}
	}

}

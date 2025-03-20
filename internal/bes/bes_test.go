package bes

import "testing"

func TestPathFromURI(t *testing.T) {
	path, err := pathFromURI("file:///hello/world.txt")
	if err != nil {
		t.Errorf("pathFromURI error: %v", err)
	}

	if want := "/hello/world.txt"; want != path {
		t.Errorf("wanted %v got %v", want, path)
	}
}

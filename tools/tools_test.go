package tools

import "testing"

func TestBuildFilename_ValidTitle(t *testing.T) {
	title := "Valid Title"
	dir := "some/dir"
	id := "123"
	expected := "some/dir/123-valid-title.md"
	result := BuildFilename(title, dir, id)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestBuildFilename_TitleWithSpecialChars(t *testing.T) {
	title := "Title/With/Special'Chars"
	dir := "some/dir"
	id := "123"
	expected := "some/dir/123-title-with-special-chars.md"
	result := BuildFilename(title, dir, id)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestBuildFilename_EmptyTitle(t *testing.T) {
	title := ""
	dir := "some/dir"
	id := "123"
	expected := "some/dir/123-.md"
	result := BuildFilename(title, dir, id)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestReplaceAllMultipleChars_RemovesConsecutiveChars(t *testing.T) {
	input := "aa---bb__cc==dd"
	expected := "a-b_c=d"
	result := ReplaceAllMultipleChars(input)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

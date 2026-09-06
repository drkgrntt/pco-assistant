package controllers

import (
	"reflect"
	"testing"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"
)

// --- songAttachmentViews -----------------------------------------------

func TestSongAttachmentViewsNilInputReturnsNil(t *testing.T) {
	if got := songAttachmentViews(nil); got != nil {
		t.Errorf("songAttachmentViews(nil) = %+v, want nil (so songDetailCard renders no Files block at all)", got)
	}
}

func TestSongAttachmentViewsEmptySliceReturnsNil(t *testing.T) {
	if got := songAttachmentViews([]scheduling.SongAttachmentInfo{}); got != nil {
		t.Errorf("songAttachmentViews([]) = %+v, want nil", got)
	}
}

func TestSongAttachmentViewsMapsFields(t *testing.T) {
	infos := []scheduling.SongAttachmentInfo{
		{PCOAttachmentID: "att-1", Filename: "Lead Sheet.pdf", ContentType: "application/pdf"},
		{PCOAttachmentID: "att-2", Filename: "Chart.pdf", ContentType: "application/pdf"},
	}

	got := songAttachmentViews(infos)

	want := []views.SongAttachmentView{
		{PCOAttachmentID: "att-1", Filename: "Lead Sheet.pdf"},
		{PCOAttachmentID: "att-2", Filename: "Chart.pdf"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("songAttachmentViews(%+v) = %+v, want %+v", infos, got, want)
	}
}

package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestCustomDirectUploadSizeBoundary(t *testing.T) {
	makeInput := func(size int64, count int) customUploadRequest {
		r := customUploadRequest{}
		for range count {
			r.Files = append(r.Files, struct {
				Name string `json:"name"`
				Size int64  `json:"size"`
			}{"原始声音.wav", size})
		}
		return r
	}
	for _, tc := range []struct {
		size  int64
		count int
		valid bool
	}{{2 << 30, 1, true}, {2 << 30, 20, true}, {(2 << 30) + 1, 1, false}, {0, 1, false}, {1, 21, false}, {1, 0, false}} {
		if err := validateCustomUploadRequest(makeInput(tc.size, tc.count)); (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
	}
	input := makeInput(100, 1)
	input.Files[0].Name = "video.exe"
	if validateCustomUploadRequest(input) == nil {
		t.Fatal("invalid extension accepted")
	}
}

func TestCustomDirectUploadRequiresAuthentication(t *testing.T) {
	s := &Server{}
	for _, handler := range []func(*httptest.ResponseRecorder){
		func(w *httptest.ResponseRecorder) {
			s.customMainlineUploadInit(w, httptest.NewRequest("POST", "/", nil))
		},
		func(w *httptest.ResponseRecorder) {
			s.customMainlineUploadComplete(w, httptest.NewRequest("POST", "/", nil))
		},
		func(w *httptest.ResponseRecorder) {
			s.customMainlineUploadStatus(w, httptest.NewRequest("GET", "/", nil))
		},
		func(w *httptest.ResponseRecorder) {
			s.customMainlineUploadStatus(w, httptest.NewRequest("DELETE", "/", nil))
		},
	} {
		w := httptest.NewRecorder()
		handler(w)
		if w.Code < 400 {
			t.Fatal("anonymous upload access allowed", w.Code)
		}
	}
}

package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type auditBodyFixture struct {
	reader io.Reader
	closed bool
	reads  int
}

func (b *auditBodyFixture) Read(p []byte) (int, error) {
	b.reads++
	return b.reader.Read(p)
}

func (b *auditBodyFixture) Close() error { b.closed = true; return nil }

func TestAuditJSONBodyPreserved(t *testing.T) {
	for _, size := range []int{0, 1, (64 << 10) - 1, 64 << 10, (64 << 10) + 1, 389781, (2 << 20)} {
		for _, chunked := range []bool{false, true} {
			t.Run(strconv.Itoa(size)+"/"+map[bool]string{false: "length", true: "chunked"}[chunked], func(t *testing.T) {
				metadata := `{"reason":"保存录音","tenant_id":14}`
				raw := []byte(strings.Repeat(" ", max(0, size-len(metadata))) + metadata)
				body := &auditBodyFixture{reader: bytes.NewReader(raw)}
				r := httptest.NewRequest(http.MethodPost, "/api/v1/live-agent-plans/7/versions", nil)
				r.Body = body
				r.ContentLength = int64(len(raw))
				if chunked {
					r.ContentLength = -1
				}
				r.Header.Set("Content-Type", "application/json; charset=utf-8")
				payload := readAuditJSONPayload(r)
				if len(raw) <= 64<<10 {
					if payload["reason"] != "保存录音" {
						t.Fatal("small audit metadata lost")
					}
				} else if payload != nil {
					t.Fatal("oversized audit body must not be decoded or retained")
				}
				got, err := io.ReadAll(r.Body)
				if err != nil || !bytes.Equal(got, raw) {
					t.Fatalf("business body changed: got=%d want=%d err=%v", len(got), len(raw), err)
				}
				if err := r.Body.Close(); err != nil || !body.closed {
					t.Fatal("original body close not propagated")
				}
			})
		}
	}
}

type auditErrorReader struct{ err error }

func (r auditErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestAuditJSONReadErrorPreserved(t *testing.T) {
	want := errors.New("fixture read failure")
	body := &auditBodyFixture{reader: io.MultiReader(strings.NewReader(`{"reason":"partial`), auditErrorReader{want})}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = body
	r.Header.Set("Content-Type", "application/json")
	if readAuditJSONPayload(r) != nil {
		t.Fatal("read failure decoded")
	}
	raw, err := io.ReadAll(r.Body)
	if string(raw) != `{"reason":"partial` || !errors.Is(err, want) {
		t.Fatalf("body read error was lost: %v", err)
	}
	r.Body.Close()
	if !body.closed {
		t.Fatal("close lost after read error")
	}
}

func TestAuditJSONPreservesEndpointLimitAndStrictSchema(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		limit     int64
		wantErr   bool
	}{
		{"valid-large", `{"text":"` + strings.Repeat("声", 50000) + `"}`, 1 << 20, false},
		{"endpoint-limit", `{"text":"` + strings.Repeat("声", 400000) + `"}`, 1 << 20, true},
		{"unknown-field", `{"text":"ok","unknown":1}`, 1 << 20, true},
		{"invalid-json", `{"text":"unfinished`, 1 << 20, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.raw))
			r.Header.Set("Content-Type", "application/json")
			readAuditJSONPayload(r)
			var input struct {
				Text string `json:"text"`
			}
			err := readJSONWithLimit(httptest.NewRecorder(), r, &input, tc.limit)
			if (err != nil) != tc.wantErr {
				t.Fatalf("endpoint validation changed: %v", err)
			}
		})
	}
}

func TestAuditJSONSkipsNonJSONBody(t *testing.T) {
	body := &auditBodyFixture{reader: strings.NewReader("multipart-fixture")}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Body = body
	r.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	if readAuditJSONPayload(r) != nil || body.reads != 0 || r.Body != body {
		t.Fatal("non-JSON body consumed")
	}
}

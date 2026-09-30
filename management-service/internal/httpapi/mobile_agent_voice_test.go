package httpapi

import (
	"mime/multipart"
	"net/textproto"
	"testing"
)

func TestMobileAgentVoiceFormatUsesFilenameAndMimeFallback(t *testing.T) {
	webm := &multipart.FileHeader{Filename: "voice.webm", Header: textproto.MIMEHeader{}}
	if ext, contentType := mobileAgentVoiceFormat(webm); ext != ".webm" || contentType != "audio/webm" {
		t.Fatalf("webm format=%q/%q", ext, contentType)
	}
	mp4 := &multipart.FileHeader{Filename: "voice", Header: textproto.MIMEHeader{"Content-Type": []string{"audio/mp4;codecs=mp4a.40.2"}}}
	if ext, contentType := mobileAgentVoiceFormat(mp4); ext != ".m4a" || contentType != "audio/mp4" {
		t.Fatalf("mp4 format=%q/%q", ext, contentType)
	}
	unknown := &multipart.FileHeader{Filename: "voice.bin", Header: textproto.MIMEHeader{"Content-Type": []string{"application/octet-stream"}}}
	if ext, contentType := mobileAgentVoiceFormat(unknown); ext != "" || contentType != "" {
		t.Fatalf("unknown format=%q/%q", ext, contentType)
	}
}

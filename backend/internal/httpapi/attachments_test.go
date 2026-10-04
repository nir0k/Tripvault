package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// attach sends a file to a place the way the browser does.
func attach(t *testing.T, s *Server, itemID, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return attachDescribed(t, s, itemID, name, data, "")
}

// attachDescribed sends a file with the line describing it, in a field before
// the file as the browser sends it; an empty description sends no field.
func attachDescribed(t *testing.T, s *Server, itemID, name string, data []byte,
	description string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if description != "" {
		if err := form.WriteField("description", description); err != nil {
			t.Fatalf("write the description: %v", err)
		}
	}
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create the file part: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write the file part: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("close the form: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/"+itemID+"/attachments", body)
	request.Header.Set("Authorization", "Bearer good")
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	s.routes().ServeHTTP(recorder, request)
	return recorder
}

// newAttachmentServer builds a plan whose one place takes attachments, for
// somebody in the given role.
func newAttachmentServer(role domain.TripRole) (*Server, *fakeDocuments) {
	s, docs := newDocumentServer(role)
	s.attachmentMaxBytes = 1024
	s.mediaTripQuota = 4096
	return s, docs
}

// ticket is a small PDF, as a booking arrives.
var ticket = []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n")

// TestAttachmentIsStoredListedAndDownloaded checks a ticket is attached to a
// place, listed on it in the document and downloaded back as it was sent,
// always as a download.
func TestAttachmentIsStoredListedAndDownloaded(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleEditor)
	recorder := attach(t, s, docs.place.ID.String(), `C:\Users\me\Downloads\Ferry ticket.pdf`, ticket)
	if recorder.Code != http.StatusOK {
		t.Fatalf("attach a ticket: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.attachmentQuota != 4096 {
		t.Errorf("the trip's allowance was %d, want 4096", docs.attachmentQuota)
	}
	document := decodeDocument(t, recorder.Body.Bytes())
	listed := document.Days[0].Items[0].Attachments
	if len(listed) != 1 {
		t.Fatalf("the place lists %d attachments, want 1", len(listed))
	}
	if listed[0].OriginalName != "Ferry ticket.pdf" || listed[0].MIME != "application/pdf" ||
		listed[0].Size != int64(len(ticket)) {
		t.Errorf("the place lists %+v", listed[0])
	}
	if docs.attachments[0].UploadedBy == nil {
		t.Error("the uploader was not recorded")
	}

	recorder = send(s, http.MethodGet, "/api/v1/attachments/"+listed[0].ID+"/file", "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("download the ticket: %d %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Equal(recorder.Body.Bytes(), ticket) {
		t.Error("the download differs from the upload")
	}
	header := recorder.Header()
	if header.Get("Content-Type") != "application/pdf" {
		t.Errorf("sent as %q", header.Get("Content-Type"))
	}
	if header.Get("Content-Disposition") != `attachment; filename="Ferry ticket.pdf"` {
		t.Errorf("disposition %q", header.Get("Content-Disposition"))
	}
	if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("the download is not kept from being rendered: %v", header)
	}

	recorder = send(s, http.MethodDelete, "/api/v1/attachments/"+listed[0].ID, "good", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete the ticket: %d %s", recorder.Code, recorder.Body.String())
	}
	if items := decodeDocument(t, recorder.Body.Bytes()).Days[0].Items; len(items[0].Attachments) != 0 {
		t.Errorf("the place still lists %d attachments", len(items[0].Attachments))
	}
}

// TestAttachmentRefusals checks what a place does not take: kinds of file left
// out, a name that disagrees with the bytes, a file too large, a second copy,
// one file more than a place carries and a stay mark.
func TestAttachmentRefusals(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleOwner)
	place := docs.place.ID.String()
	cases := []struct {
		name   string
		file   string
		data   []byte
		status int
		code   string
	}{
		{"program", "setup.exe", []byte("MZ\x90\x00\x03\x00"), http.StatusUnsupportedMediaType, "unsupported_attachment"},
		{"pdf named as text", "ticket.txt", ticket, http.StatusUnsupportedMediaType, "unsupported_attachment"},
		{"script named as text", "notes.txt", []byte("#!/bin/sh\necho\n"), http.StatusUnsupportedMediaType,
			"unsupported_attachment"},
		{"gif", "scan.gif", []byte("GIF89a\x01\x00"), http.StatusUnsupportedMediaType, "unsupported_attachment"},
		{"too large", "big.pdf", append([]byte("%PDF-1.7\n"), make([]byte, 2048)...),
			http.StatusRequestEntityTooLarge, "file_too_large"},
		{"empty", "empty.pdf", nil, http.StatusUnprocessableEntity, "validation_failed"},
		{"nameless", "", ticket, http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := attach(t, s, place, tc.file, tc.data)
			if recorder.Code != tc.status || errorCode(t, recorder) != tc.code {
				t.Errorf("got %d %s, want %d %s", recorder.Code, recorder.Body.String(), tc.status, tc.code)
			}
		})
	}
	if len(docs.attachments) != 0 {
		t.Fatalf("%d refused files were stored", len(docs.attachments))
	}

	if recorder := attach(t, s, place, "ticket.pdf", ticket); recorder.Code != http.StatusOK {
		t.Fatalf("attach a ticket: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := attach(t, s, place, "copy.pdf", ticket); recorder.Code != http.StatusConflict ||
		errorCode(t, recorder) != "duplicate_attachment" {
		t.Errorf("a second copy: %d %s", recorder.Code, recorder.Body.String())
	}
	for index := 1; index < domain.MaxItemAttachments; index++ {
		docs.attachments = append(docs.attachments, domain.Attachment{ID: uuid.New(), ItemID: docs.place.ID,
			Checksum: []byte{byte(index)}})
	}
	if recorder := attach(t, s, place, "notes.txt", []byte("Gate B")); recorder.Code != http.StatusUnprocessableEntity ||
		errorCode(t, recorder) != "validation_failed" {
		t.Errorf("one file more than a place carries: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := attach(t, s, docs.anchor.ID.String(), "ticket.pdf", ticket); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a stay mark took an attachment: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestAttachmentQuota checks an attachment counts against the trip's allowance.
func TestAttachmentQuota(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleOwner)
	s.mediaTripQuota = 10
	recorder := attach(t, s, docs.place.ID.String(), "ticket.pdf", ticket)
	if recorder.Code != http.StatusConflict || errorCode(t, recorder) != "media_quota" {
		t.Errorf("a file over the allowance: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestAttachmentRoles checks a viewer downloads what is attached but neither
// attaches nor removes anything.
func TestAttachmentRoles(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleViewer)
	stored := domain.Attachment{ID: uuid.New(), DocumentID: docs.document.ID, ItemID: docs.place.ID,
		OriginalName: "ticket.pdf", MIME: "application/pdf", Size: int64(len(ticket))}
	docs.attachments = []domain.Attachment{stored}
	docs.attachmentFiles = map[uuid.UUID][]byte{stored.ID: ticket}

	if recorder := attach(t, s, docs.place.ID.String(), "notes.txt", []byte("Gate B")); recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer attached a file: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodDelete, "/api/v1/attachments/"+stored.ID.String(), "good", ""); recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer removed a file: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodGet, "/api/v1/attachments/"+stored.ID.String()+"/file", "good", ""); recorder.Code != http.StatusOK {
		t.Errorf("a viewer could not download a file: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := send(s, http.MethodGet, "/api/v1/attachments/"+uuid.NewString()+"/file", "good", ""); recorder.Code != http.StatusNotFound {
		t.Errorf("an unknown attachment: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestSharedDocumentHidesAttachments checks a read-only link never lists what
// is attached to a place, and has no way to download it.
func TestSharedDocumentHidesAttachments(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleOwner)
	stored := domain.Attachment{ID: uuid.New(), DocumentID: docs.document.ID, ItemID: docs.place.ID,
		OriginalName: "ticket.pdf", MIME: "application/pdf", Size: int64(len(ticket))}
	docs.attachments = []domain.Attachment{stored}
	docs.attachmentFiles = map[uuid.UUID][]byte{stored.ID: ticket}
	link := createLink(t, s, docs.document.TripID.String(), `{"include_private_media":true,"allow_download":true}`)

	recorder := sendShared(s, "/api/v1/shared/document", link.Token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read the shared document: %d %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("ticket.pdf")) {
		t.Error("the shared document names the attachment")
	}
	if listed := decodeDocument(t, recorder.Body.Bytes()).Days[0].Items[0].Attachments; len(listed) != 0 {
		t.Errorf("the shared document lists %d attachments", len(listed))
	}
	for _, path := range []string{
		"/api/v1/shared/attachments/" + stored.ID.String() + "/file",
		"/api/v1/attachments/" + stored.ID.String() + "/file",
	} {
		if recorder := sendShared(s, path, link.Token); recorder.Code == http.StatusOK {
			t.Errorf("a link downloaded the attachment from %s", path)
		}
	}
}

// TestAttachmentDescription checks a file is attached with the line describing
// it, tidied into one line, that the line is changed and cleared afterwards,
// and that a line over 150 characters is refused either way.
func TestAttachmentDescription(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleEditor)
	place := docs.place.ID.String()
	recorder := attachDescribed(t, s, place, "ticket.pdf", ticket, "  Return tickets,\n  both of us  ")
	if recorder.Code != http.StatusOK {
		t.Fatalf("attach a described ticket: %d %s", recorder.Code, recorder.Body.String())
	}
	listed := decodeDocument(t, recorder.Body.Bytes()).Days[0].Items[0].Attachments
	if len(listed) != 1 || listed[0].Description != "Return tickets, both of us" {
		t.Fatalf("the place lists %+v", listed)
	}

	path := "/api/v1/attachments/" + listed[0].ID
	if recorder := send(s, http.MethodPatch, path, "good", `{"description":"Ferry, 23 June"}`); recorder.Code != http.StatusOK {
		t.Fatalf("change the description: %d %s", recorder.Code, recorder.Body.String())
	}
	if docs.attachments[0].Description != "Ferry, 23 June" {
		t.Errorf("the description became %q", docs.attachments[0].Description)
	}
	if recorder := send(s, http.MethodPatch, path, "good", `{"description":""}`); recorder.Code != http.StatusOK ||
		docs.attachments[0].Description != "" {
		t.Errorf("clearing the description: %d, left %q", recorder.Code, docs.attachments[0].Description)
	}

	exact := strings.Repeat("я", domain.MaxAttachmentDescription)
	if recorder := send(s, http.MethodPatch, path, "good", `{"description":"`+exact+`"}`); recorder.Code != http.StatusOK {
		t.Errorf("a description of exactly 150 characters: %d %s", recorder.Code, recorder.Body.String())
	}
	long := exact + "я"
	if recorder := send(s, http.MethodPatch, path, "good", `{"description":"`+long+`"}`); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a description of 151 characters: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := attachDescribed(t, s, place, "notes.txt", []byte("Gate B"), long); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("a file with a description of 151 characters: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(docs.attachments) != 1 {
		t.Errorf("a refused file was stored: %d attachments", len(docs.attachments))
	}
}

// TestViewerCannotDescribeAttachment checks a viewer cannot change what an
// attachment is described as.
func TestViewerCannotDescribeAttachment(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleViewer)
	stored := domain.Attachment{ID: uuid.New(), DocumentID: docs.document.ID, ItemID: docs.place.ID,
		OriginalName: "ticket.pdf", MIME: "application/pdf", Size: int64(len(ticket))}
	docs.attachments = []domain.Attachment{stored}
	recorder := send(s, http.MethodPatch, "/api/v1/attachments/"+stored.ID.String(), "good", `{"description":"x"}`)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("a viewer described a file: %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestReportPlacesTakeNoAttachments checks a report's place refuses a file:
// tickets and bookings belong to the plan.
func TestReportPlacesTakeNoAttachments(t *testing.T) {
	s, docs := newAttachmentServer(domain.RoleEditor)
	docs.document.Kind = domain.DocumentReport
	recorder := attach(t, s, docs.place.ID.String(), "receipt.pdf", ticket)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"reason":"plan_only"`) ||
		len(docs.attachments) != 0 {
		t.Errorf("a report's place took a file: %d %s", recorder.Code, recorder.Body.String())
	}
}

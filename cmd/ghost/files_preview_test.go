package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/uploads"
)

func TestPreviewUploadByKind(t *testing.T) {
	ws := t.TempDir()
	save := func(name string, data []byte) uploads.Item {
		it, err := uploads.Save(ws, data, name, "", "test")
		if err != nil {
			t.Fatal(err)
		}
		return it
	}

	csv := previewUpload(ws, save("spend.csv", []byte("item,cost\ncoffee,4\n")))
	if csv["previewable"] != true || !strings.Contains(csv["content"].(string), "coffee") {
		t.Fatalf("a spreadsheet previews as its text: %v", csv)
	}
	txt := previewUpload(ws, save("note.txt", []byte("remember the milk")))
	if txt["previewable"] != true || txt["content"] != "remember the milk" {
		t.Fatalf("text previews as itself: %v", txt)
	}
	png := previewUpload(ws, save("p.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")))
	if png["previewable"] != true || png["image_base64"] == "" {
		t.Fatalf("a photo previews as itself: %v", png)
	}
	other := previewUpload(ws, save("blob.xyz", []byte{1, 2, 3, 4, 5, 6, 7, 8}))
	if other["previewable"] != false || !strings.Contains(other["reason"].(string), "Open it instead") {
		t.Fatalf("an unknown file says so and points to Open: %v", other)
	}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		pdf := "%PDF-1.1\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
			"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 100]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
			"4 0 obj<</Length 44>>stream\nBT /F1 18 Tf 20 50 Td (Quarterly total 4210) Tj ET\nendstream endobj\n" +
			"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R/Size 6>>\n%%EOF\n"
		p := previewUpload(ws, save("report.pdf", []byte(pdf)))
		if p["previewable"] != true || p["extracted"] != true || !strings.Contains(p["content"].(string), "4210") {
			t.Fatalf("a PDF previews as the text Ghost reads from it: %v", p)
		}
	}
}

package tools

import (
	"archive/zip"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxDocumentBytes   = 50 * 1024 * 1024 // 50MB limit matching Hermes
	pandocTimeout      = 30 * time.Second
	pandocRetrySeconds = 300.0
)

var (
	pandocAvailable      *bool
	pandocMu             sync.Mutex
	pandocInstallFailed  bool
	pandocInstallFailedAt time.Time
)

type DocParserTool struct {
	workspace string
}

func NewDocParserTool(workspace string) *DocParserTool {
	return &DocParserTool{
		workspace: workspace,
	}
}

func (t *DocParserTool) Name() string {
	return "doc_parser"
}

func (t *DocParserTool) Description() string {
	return "Extract text from documents: .docx, .xlsx, .ipynb (native), PDF, PPTX, ODT, ODS, ODP, RTF, EPUB, HTML, TXT, CSV, JSON, XML (via pandoc)."
}

func (t *DocParserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the document file",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"docx", "xlsx", "ipynb", "pdf", "pptx", "ppt", "odt", "ods", "odp", "rtf", "epub", "html", "txt", "csv", "json", "xml", "auto"},
				"description": "Document format (auto-detected from extension if not specified)",
			},
		},
		"required": []string{"file_path"},
	}
}

func (t *DocParserTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return ErrorResult("file_path is required")
	}

	format, _ := args["format"].(string)
	if format == "" || format == "auto" {
		format = detectFormat(filePath)
	}

	if !isAccessible(filePath, t.workspace) {
		return ErrorResult("file path is not accessible")
	}

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("file not found: %s", filePath))
	}
	if info != nil && info.Size() > maxDocumentBytes {
		return ErrorResult(fmt.Sprintf("file too large (%d bytes, limit is %d)", info.Size(), maxDocumentBytes))
	}

	var content string

	switch format {
	case "docx":
		content, err = t.parseDocx(filePath)
	case "xlsx":
		content, err = t.parseXlsx(filePath)
	case "ipynb":
		content, err = t.parseIpynb(filePath)
	default:
		content, err = convertWithPandoc(filePath, format)
	}

	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse %s: %v", format, err)).WithError(err)
	}

	result := map[string]interface{}{
		"file_path": filePath,
		"format":    format,
		"content":   content,
		"length":    len(content),
	}
	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}

func (t *DocParserTool) parseDocx(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open docx: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("failed to open document.xml: %w", err)
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return "", fmt.Errorf("failed to read document.xml: %w", err)
			}

			return extractDocxText(data)
		}
	}

	return "", fmt.Errorf("word/document.xml not found in docx")
}

type docxDocument struct {
	XMLName xml.Name    `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main document"`
	Body    docxBody    `xml:"body"`
}

type docxBody struct {
	Paragraphs []docxParagraph `xml:"p"`
}

type docxParagraph struct {
	Runs     []docxRun `xml:"r"`
	ParagraphProperties *docxParagraphProperties `xml:"pPr"`
}

type docxParagraphProperties struct {
	Spacing *docxSpacing `xml:"spacing"`
}

type docxSpacing struct {
	After string `xml:"after,attr"`
	Line  string `xml:"line,attr"`
}

type docxRun struct {
	Text         string `xml:"t"`
	RPr          *docxRunProperties `xml:"rPr"`
}

type docxRunProperties struct {
	B    *docxEmpty `xml:"b"`
	I    *docxEmpty `xml:"i"`
	U    *docxUnderline `xml:"u"`
}

type docxEmpty struct{}

type docxUnderline struct {
	Val string `xml:"val,attr"`
}

func extractDocxText(data []byte) (string, error) {
	var doc docxDocument
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("failed to parse XML: %w", err)
	}

	var sb strings.Builder
	for i, p := range doc.Body.Paragraphs {
		if i > 0 {
			sb.WriteString("\n")
		}

		for _, run := range p.Runs {
			text := run.Text
			if text == "" {
				continue
			}

			if run.RPr != nil {
				if run.RPr.B != nil {
					text = "**" + text + "**"
				}
				if run.RPr.I != nil {
					text = "*" + text + "*"
				}
				if run.RPr.U != nil && run.RPr.U.Val != "none" {
					text = "__" + text + "__"
				}
			}

			sb.WriteString(text)
		}
	}

	return sb.String(), nil
}

func (t *DocParserTool) parseXlsx(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open xlsx: %w", err)
	}
	defer r.Close()

	var sharedStrings []string
	for _, f := range r.File {
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			sharedStrings, _ = extractSharedStrings(data)
			break
		}
	}

	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			content, err := extractSheetText(data, sharedStrings)
			if err == nil && content != "" {
				return content, nil
			}
		}
	}

	return "", fmt.Errorf("no worksheets found in xlsx")
}

type sharedStrings struct {
	XMLName xml.Name         `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main sst"`
	Items   []sharedStringItem `xml:"si"`
}

type sharedStringItem struct {
	Text string `xml:"t"`
	RichText []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func extractSharedStrings(data []byte) ([]string, error) {
	var sst sharedStrings
	if err := xml.Unmarshal(data, &sst); err != nil {
		return nil, err
	}

	var result []string
	for _, item := range sst.Items {
		if item.Text != "" {
			result = append(result, item.Text)
		} else if len(item.RichText) > 0 {
			var sb strings.Builder
			for _, rt := range item.RichText {
				sb.WriteString(rt.Text)
			}
			result = append(result, sb.String())
		}
	}
	return result, nil
}

type worksheet struct {
	XMLName xml.Name      `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main worksheet"`
	Rows    []worksheetRow `xml:"sheetData>row"`
}

type worksheetRow struct {
	Cells []worksheetCell `xml:"c"`
}

type worksheetCell struct {
	Type  string `xml:"t,attr"`
	Value string `xml:"v"`
}

func extractSheetText(data []byte, sharedStrings []string) (string, error) {
	var ws worksheet
	if err := xml.Unmarshal(data, &ws); err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, row := range ws.Rows {
		var rowText []string
		for _, cell := range row.Cells {
			value := cell.Value
			if cell.Type == "s" {
				var idx int
				if _, err := fmt.Sscanf(value, "%d", &idx); err == nil && idx < len(sharedStrings) {
					value = sharedStrings[idx]
				}
			}
			rowText = append(rowText, value)
		}
		if len(rowText) > 0 {
			sb.WriteString(strings.Join(rowText, "\t"))
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}

func (t *DocParserTool) parseIpynb(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read ipynb: %w", err)
	}

	var nb struct {
		Cells []struct {
			CellType string   `json:"cell_type"`
			Source   []string `json:"source"`
		} `json:"cells"`
	}

	if err := json.Unmarshal(data, &nb); err != nil {
		return "", fmt.Errorf("failed to parse ipynb JSON: %w", err)
	}

	var sb strings.Builder
	for i, cell := range nb.Cells {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}

		sb.WriteString(fmt.Sprintf("# %s (Cell %d)\n\n", cell.CellType, i+1))
		for _, line := range cell.Source {
			sb.WriteString(line)
		}
	}

	return sb.String(), nil
}

func detectFormat(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".docx":
		return "docx"
	case ".xlsx", ".xls":
		return "xlsx"
	case ".ipynb":
		return "ipynb"
	case ".pdf":
		return "pdf"
	case ".pptx", ".ppt":
		return "pptx"
	case ".odt":
		return "odt"
	case ".ods":
		return "ods"
	case ".odp":
		return "odp"
	case ".rtf":
		return "rtf"
	case ".epub":
		return "epub"
	case ".html", ".htm":
		return "html"
	case ".txt", ".md", ".markdown":
		return "txt"
	case ".csv":
		return "csv"
	case ".json":
		return "json"
	case ".xml":
		return "xml"
	default:
		return ""
	}
}

// ensurePandoc lazily detects or installs pandoc, returning the path.
// Follows Hermes's lazy dependency pattern with retry on failure.
func ensurePandoc() (string, error) {
	pandocMu.Lock()
	defer pandocMu.Unlock()

	// Return cached result if available
	if pandocAvailable != nil {
		if *pandocAvailable {
			path, err := exec.LookPath("pandoc")
			if err == nil {
				return path, nil
			}
		}
		return "", fmt.Errorf("pandoc not found")
	}

	// Check if we recently failed and retry window hasn't elapsed
	if pandocInstallFailed && time.Since(pandocInstallFailedAt).Seconds() < pandocRetrySeconds {
		return "", fmt.Errorf("pandoc installation failed recently, retrying in %ds", int(pandocRetrySeconds-time.Since(pandocInstallFailedAt).Seconds()))
	}

	// Try to find pandoc first
	if path, err := exec.LookPath("pandoc"); err == nil {
		t := true
		pandocAvailable = &t
		pandocInstallFailed = false
		return path, nil
	}

	// Try to install pandoc
	if err := installPandoc(); err != nil {
		pandocInstallFailed = true
		pandocInstallFailedAt = time.Now()
		return "", fmt.Errorf("pandoc not found and auto-install failed: %w", err)
	}

	// Verify installation succeeded
	if path, err := exec.LookPath("pandoc"); err == nil {
		t := true
		pandocAvailable = &t
		pandocInstallFailed = false
		return path, nil
	}

	pandocInstallFailed = true
	pandocInstallFailedAt = time.Now()
	return "", fmt.Errorf("pandoc installation completed but binary not found")
}

// installPandoc attempts to install pandoc via apt-get.
func installPandoc() error {
	cmd := exec.Command("sudo", "apt-get", "install", "-y", "pandoc")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// convertWithPandoc converts a document using pandoc.
func convertWithPandoc(filePath, format string) (string, error) {
	pandocPath, err := ensurePandoc()
	if err != nil {
		return "", err
	}

	// Map format names to pandoc input formats
	pandocFormat := format
	switch format {
	case "pptx", "ppt":
		pandocFormat = "pptx"
	case "txt", "csv", "json", "xml":
		pandocFormat = "plain"
	}

	ctx, cancel := context.WithTimeout(context.Background(), pandocTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pandocPath, "-f", pandocFormat, "-t", "markdown", "--wrap=none", filePath)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("pandoc conversion timed out after %v", pandocTimeout)
		}
		return "", fmt.Errorf("pandoc conversion failed: %w", err)
	}

	return string(output), nil
}

func isAccessible(filePath, workspace string) bool {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}

	return strings.HasPrefix(absPath, absWorkspace) || !strings.HasPrefix(absPath, "/")
}

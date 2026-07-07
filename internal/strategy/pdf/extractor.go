package pdf

// TextRun is one run of text from a PDF with the font size and position needed to infer
// structure (headings versus body). FontSize is the rendered size in points; X and Y are the
// run's left and top position in PDF points (higher Y is higher on the page).
type TextRun struct {
	Text     string
	FontSize float64
	X        float64
	Y        float64
	Page     int
}

// PDFTextExtractor extracts positioned, font-annotated text runs from a PDF's raw bytes. It
// is injected into the PDF strategy so the underlying extraction engine can be swapped
// without touching the strategy. Returning runs (rather than flat text) keeps the
// heading-inference logic engine-independent.
type PDFTextExtractor interface {
	ExtractRuns(content []byte) ([]TextRun, error)
}
